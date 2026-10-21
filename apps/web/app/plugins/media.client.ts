import type { Attachment } from "@tdc/types";

import { downloadKey } from "#layers/ui/app/composables/useDownload";
import { mediaBaseKey } from "#layers/ui/app/composables/useMediaBase";
import { mediaFetchKey } from "#layers/ui/app/composables/useMediaFetch";
import { uploadTransferKey } from "#layers/ui/app/composables/useUploadTransfer";

/**
 * Installs the web client's media base, byte fetcher, upload transfer, and
 * download.
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
        throw new Error(nuxtApp.$ts("attachment.fileUnreadable", { status: response.status }));
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
            handlers.onError(nuxtApp.$ts("attachment.replyUnreadable"));
          }
        } else {
          handlers.onError(messageFrom(request, nuxtApp.$ts));
        }
      });

      request.addEventListener("error", () =>
        handlers.onError(nuxtApp.$ts("attachment.uploadUnreachable")),
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

    /*
     * A plain link, followed. The browser carries the session cookie on it and
     * streams the response straight to disk, which is better than any copy
     * this could make in memory first.
     */
    nuxtApp.vueApp.provide(downloadKey, async (path, filename) => {
      const link = document.createElement("a");
      link.href = `${base}${path}`;
      link.download = filename;
      link.click();
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
function messageFrom(
  request: XMLHttpRequest,
  say: (key: string, params?: Record<string, number>) => string,
): string {
  try {
    const body = JSON.parse(request.responseText);
    if (typeof body?.message === "string" && body.message) return body.message;
  } catch {
    // Not JSON; fall through to the status.
  }

  if (request.status === 413) return say("attachment.tooLarge");
  if (request.status === 415) return say("attachment.notAccepted");
  if (request.status === 401) return say("attachment.sessionExpired");
  if (request.status === 403) return say("attachment.noUploadPermission");

  return say("attachment.uploadFailed", { status: request.status });
}
