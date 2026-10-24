import { createApiClient } from "@tdc/types";

import { downloadKey } from "#layers/ui/app/composables/useDownload";
import { mediaBaseKey } from "#layers/ui/app/composables/useMediaBase";
import { mediaFetchKey } from "#layers/ui/app/composables/useMediaFetch";
import { saveFileKey } from "#layers/ui/app/composables/useSaveFile";
import { uploadTransferKey } from "#layers/ui/app/composables/useUploadTransfer";
import * as Api from "#bindings/api";

import { createWailsTransport } from "~/lib/transport";

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
          throw new Error(nuxtApp.$ts("attachment.fileUnreadable", { status: response.status }));
        }
        return response.arrayBuffer();
      }

      const result = await Api.Service.FetchMedia(path);
      if (!result.ok) {
        throw new Error(nuxtApp.$ts("attachment.fileUnreadable", { status: result.status }));
      }
      // The runtime hands []byte across as base64; decode it back to bytes.
      return bytesFromBase64(result.data ?? "");
    });

    /*
     * One piece through Go, as base64 in JSON — the ordinary request path,
     * which carries the session. Only one piece is ever in memory, so a file
     * of any size can be sent, and progress moves a piece at a time where it
     * used to show nothing until the whole file had gone. A piece already
     * handed to Go cannot be recalled; stopping takes effect at the next one.
     */
    const pieces = createApiClient(createWailsTransport(Api.Service));
    nuxtApp.vueApp.provide(uploadTransferKey, async (uploadId, piece, data, onProgress, signal) => {
      if (signal.aborted) throw new Error("stopped");
      const bytes = new Uint8Array(await data.arrayBuffer());
      await pieces.put(`/api/uploads/${uploadId}/pieces/${piece}`, {
        data: base64FromBytes(bytes),
      });
      onProgress(data.size);
    });

    /*
     * A server file, saved. A link would leave the webview without the
     * session, so Go asks where to put it with the system's own dialog and
     * streams it there — never through memory, so a file of any size saves.
     */
    nuxtApp.vueApp.provide(downloadKey, async (path, filename) => {
      await Api.Service.SaveMedia(path, filename);
    });

    /*
     * A file made here — a call's recording — saved through Go: the webview
     * does not save a download it is handed on every platform. A piece at a
     * time, so a long recording never crosses the bridge whole.
     */
    nuxtApp.vueApp.provide(saveFileKey, async (file, filename) => {
      const handle = await Api.Service.BeginLocalSave(filename);
      if (!handle) return false;
      try {
        const piece = 4 * 1024 * 1024;
        for (let start = 0; start < file.size; start += piece) {
          const bytes = new Uint8Array(await file.slice(start, start + piece).arrayBuffer());
          await Api.Service.WriteLocalSave(handle, base64FromBytes(bytes));
        }
        await Api.Service.FinishLocalSave(handle);
        return true;
      } catch (cause) {
        void Api.Service.AbandonLocalSave(handle);
        throw cause;
      }
    });
  },
});

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
