import type { Attachment } from "@tdc/types";

import { mediaBaseKey } from "#layers/ui/app/composables/useMediaBase";
import { mediaFetchKey } from "#layers/ui/app/composables/useMediaFetch";
import { uploadTransferKey } from "#layers/ui/app/composables/useUploadTransfer";

/**
 * Installs the web client's media base, byte fetcher, and upload transfer.
 *
 * The base is the configured API base — empty in production, where the
 * interface and the API share an origin and a relative path resolves against
 * the page. The fetcher and the uploader carry the session cookie the browser
 * holds. The desktop client provides its own three, because there the session
 * is not in the webview to carry, and the transfer goes through Go.
 *
 * The upload uses XMLHttpRequest rather than fetch on purpose: fetch reports
 * progress on the response but not on the request body, so an upload through it
 * can only be an indeterminate spinner. The bookkeeping around this — the list,
 * retries, what counts as done — is in the shared useUploads; only the transfer
 * is here.
 */
export default defineNuxtPlugin({
  name: "tdc:media",
  enforce: "pre",
  setup(nuxtApp) {
    const base = useRuntimeConfig().public.apiBase ?? "";

    nuxtApp.vueApp.provide(mediaBaseKey, () => base);

    nuxtApp.vueApp.provide(mediaFetchKey, async (path: string) => {
      const url = /^https?:\/\//i.test(path) ? path : `${base}${path}`;
      const response = await fetch(url, { credentials: "include" });
      if (!response.ok) {
        throw new Error(`The file could not be read (${response.status}).`);
      }
      return response.arrayBuffer();
    });

    nuxtApp.vueApp.provide(uploadTransferKey, (roomId, file, handlers) => {
      const request = new XMLHttpRequest();

      const form = new FormData();
      form.append("file", file);

      request.upload.addEventListener("progress", (event) => {
        // lengthComputable is false when the browser cannot know the total —
        // rare for a plain file, but the guard keeps the bar from jumping to a
        // meaningless value.
        if (event.lengthComputable) handlers.onProgress(event.loaded);
      });

      request.addEventListener("load", () => {
        if (request.status >= 200 && request.status < 300) {
          try {
            handlers.onDone(JSON.parse(request.responseText) as Attachment);
          } catch {
            handlers.onError("The server's reply could not be read.");
          }
        } else {
          handlers.onError(messageFrom(request));
        }
      });

      request.addEventListener("error", () =>
        handlers.onError("The upload could not reach the server."),
      );

      // Fires on abort() as well as on a browser-initiated cancellation, so
      // cancelling is not reported as a failure.
      request.addEventListener("abort", () => handlers.onCancel());

      request.open("POST", `${base}/api/rooms/${roomId}/attachments`);
      // The session is a cookie, and XMLHttpRequest does not send credentials
      // cross-origin unless told to.
      request.withCredentials = true;
      request.send(form);

      return { abort: () => request.abort() };
    });
  },
});

/**
 * Extracts the server's explanation from a failed upload.
 *
 * A refusal names the limit it broke — "file size limit is 26214400, supplied
 * 31457280" — which is what someone needs in order to act. Falling back to the
 * status code alone would throw that away.
 */
function messageFrom(request: XMLHttpRequest): string {
  try {
    const body = JSON.parse(request.responseText);
    if (typeof body?.message === "string" && body.message) return body.message;
  } catch {
    // Not JSON; fall through to the status.
  }

  if (request.status === 413) return "That file is larger than this server allows.";
  if (request.status === 415) return "This server does not accept that kind of file.";
  if (request.status === 401) return "Your session has expired. Sign in and try again.";
  if (request.status === 403) return "You do not have permission to upload here.";

  return `The upload failed (${request.status}).`;
}
