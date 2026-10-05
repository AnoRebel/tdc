import type { Attachment } from "@tdc/types";

import { downloadKey } from "#layers/ui/app/composables/useDownload";
import { mediaBaseKey } from "#layers/ui/app/composables/useMediaBase";
import { mediaFetchKey } from "#layers/ui/app/composables/useMediaFetch";
import { uploadTransferKey } from "#layers/ui/app/composables/useUploadTransfer";
import * as Api from "#bindings/api";

/**
 * Installs the desktop client's media base, byte fetcher, upload transfer, and
 * download.
 *
 * The session is in the Go client's cookie jar, not the webview, so the webview
 * cannot fetch, upload or save authenticated files on its own — the seams the
 * shared layer asks for are all filled by asking Go to do it.
 *
 * Synchronous on purpose. An async plugin that awaited the current server here
 * would both hold up the others and capture the server as it was at startup,
 * before one was chosen. The base is read live instead, each time a URL is
 * built, so it is right after signing in changes which server is current.
 */
export default defineNuxtPlugin({
  name: "tdc:media",
  enforce: "pre",
  setup(nuxtApp) {
    // The chosen server, kept current. A getter rather than a value: it is
    // empty before a server is chosen and set once one is, so reading it each
    // time a URL is built keeps it right across signing in. The Wails call that
    // resolves it is async, so the getter returns the last known value and
    // refreshes for next time — for <img src>, which cannot await, that is the
    // honest tradeoff, and the value only changes when the server does.
    const base = ref("");
    const refreshBase = () => {
      void Api.Service.CurrentServer().then((current) => {
        base.value = current || "";
      });
    };
    refreshBase();

    nuxtApp.vueApp.provide(mediaBaseKey, () => {
      refreshBase();
      return base.value;
    });

    nuxtApp.vueApp.provide(mediaFetchKey, async (path: string) => {
      // An absolute URL is a public one — a GIF a provider returned, say — that
      // needs no session, so the webview fetches it directly. Only server paths,
      // which need the session in Go's jar, go through Go.
      if (/^https?:\/\//i.test(path)) {
        const response = await fetch(path);
        if (!response.ok) {
          throw new Error(`The file could not be read (${response.status}).`);
        }
        return response.arrayBuffer();
      }

      const result = await Api.Service.FetchMedia(path);
      if (!result.ok) {
        throw new Error(`The file could not be read (${result.status}).`);
      }
      // The runtime hands []byte across as base64; decode it back to bytes.
      return bytesFromBase64(result.data ?? "");
    });

    nuxtApp.vueApp.provide(uploadTransferKey, (roomId, file, handlers) => {
      let cancelled = false;

      // One round trip through Go, with no progress events — the honest thing
      // on the desktop rather than a fraction invented from nothing. The tray's
      // progress bar already tolerates a transfer that only reports its end.
      void (async () => {
        try {
          const bytes = new Uint8Array(await file.arrayBuffer());
          const response = await Api.Service.Upload(roomId, file.name, base64FromBytes(bytes));
          if (cancelled) return;

          if (response.ok && response.body) {
            handlers.onDone(response.body as unknown as Attachment);
          } else {
            handlers.onError(messageFrom(response.status ?? 0));
          }
        } catch {
          if (!cancelled) handlers.onError("The upload could not reach the server.");
        }
      })();

      return {
        abort() {
          // The request to Go cannot be recalled once sent, so cancelling only
          // stops this client from acting on the reply — the server may still
          // have stored the file. Marked cancelled so the tray shows that
          // rather than a phantom success.
          cancelled = true;
          handlers.onCancel();
        },
      };
    });

    /*
     * A server file, saved. A link would leave the webview without the
     * session, so Go fetches the bytes and they are handed over as a file from
     * here — held in memory on the way, which the size limit on FetchMedia
     * keeps bounded.
     */
    nuxtApp.vueApp.provide(downloadKey, async (path, filename) => {
      const result = await Api.Service.FetchMedia(path);
      if (!result.ok) {
        throw new Error(`The file could not be read (${result.status}).`);
      }

      const blob = new Blob([bytesFromBase64(result.data ?? "")], {
        type: result.contentType || "application/octet-stream",
      });
      const link = document.createElement("a");
      link.href = URL.createObjectURL(blob);
      link.download = filename;
      link.click();
      URL.revokeObjectURL(link.href);
    });
  },
});

/** The same explanations the web client gives, keyed on status alone. */
function messageFrom(status: number): string {
  if (status === 413) return "That file is larger than this server allows.";
  if (status === 415) return "This server does not accept that kind of file.";
  if (status === 401) return "Your session has expired. Sign in and try again.";
  if (status === 403) return "You do not have permission to upload here.";
  return `The upload failed (${status}).`;
}

/** base64 to bytes, without a Buffer this webview does not have. */
function bytesFromBase64(value: string): ArrayBuffer {
  const binary = atob(value);
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
  return bytes.buffer;
}

/** bytes to base64, in chunks so a large file does not overflow the argument
 * list of String.fromCharCode. */
function base64FromBytes(bytes: Uint8Array): string {
  let binary = "";
  const chunk = 0x8000;
  for (let i = 0; i < bytes.length; i += chunk) {
    binary += String.fromCharCode(...bytes.subarray(i, i + chunk));
  }
  return btoa(binary);
}
