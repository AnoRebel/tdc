import { ApiError, type ErrorCode } from "@tdc/types";

import { downloadKey } from "#layers/ui/app/composables/useDownload";
import { mediaBaseKey } from "#layers/ui/app/composables/useMediaBase";
import { mediaFetchKey } from "#layers/ui/app/composables/useMediaFetch";
import { saveFileKey } from "#layers/ui/app/composables/useSaveFile";
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
 * A piece of an upload goes over XMLHttpRequest rather than fetch on purpose:
 * fetch reports progress on the response but not on the request body, so a
 * piece sent through it could only jump from nothing to done. Declaring,
 * resuming and finishing an upload are the shared layer's; only how one piece
 * travels is here, as raw bytes, which costs nothing over the file itself.
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

    nuxtApp.vueApp.provide(
      uploadTransferKey,
      (uploadId, piece, data, onProgress, signal) =>
        new Promise<void>((resolve, reject) => {
          const request = new XMLHttpRequest();

          request.upload.addEventListener("progress", (event) => {
            // lengthComputable is false when the browser cannot know the
            // total; the guard keeps the bar from jumping to nonsense.
            if (event.lengthComputable) onProgress(event.loaded);
          });
          request.addEventListener("load", () => {
            if (request.status >= 200 && request.status < 300) resolve();
            else reject(refusalFrom(request, nuxtApp.$ts));
          });
          // Unreachable rather than refused: the caller retries these.
          request.addEventListener("error", () => reject(new Error("unreachable")));
          request.addEventListener("abort", () => reject(new Error("stopped")));
          signal.addEventListener("abort", () => request.abort());

          request.open("PUT", `${base}/api/uploads/${uploadId}/pieces/${piece}`);
          // The session is a cookie, and XMLHttpRequest does not send
          // credentials cross-origin unless told to.
          request.withCredentials = true;
          request.setRequestHeader("Content-Type", "application/octet-stream");
          request.send(data);
        }),
    );

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

    // A file made here — a call's recording — handed to the browser to save.
    nuxtApp.vueApp.provide(saveFileKey, async (file, filename) => {
      const link = document.createElement("a");
      link.href = URL.createObjectURL(file);
      link.download = filename;
      link.click();
      // Long enough for the download to have begun.
      setTimeout(() => URL.revokeObjectURL(link.href), 60_000);
      return true;
    });
  },
});

/**
 * The server's refusal of a piece, as the shared layer's ApiError.
 *
 * The code is what the shared loop decides on — a conflict means ask where to
 * resume, an unavailable server means wait and retry — and the message is the
 * server's own where it gave one, because a refusal that names the limit it
 * broke is what somebody needs in order to act.
 */
function refusalFrom(
  request: XMLHttpRequest,
  say: (key: string, params?: Record<string, number>) => string,
): ApiError {
  let message = "";
  try {
    const body = JSON.parse(request.responseText);
    if (typeof body?.message === "string") message = body.message;
  } catch {
    // Not JSON; the status says what it can.
  }

  const status = request.status;
  const code: ErrorCode =
    status === 401
      ? "unauthenticated"
      : status === 403
        ? "forbidden"
        : status === 404
          ? "not-found"
          : status === 409
            ? "conflict"
            : status === 429
              ? "rate-limited"
              : status >= 500 || status === 0
                ? "unavailable"
                : "invalid";

  if (!message) {
    message =
      status === 413
        ? say("attachment.tooLarge")
        : status === 401
          ? say("attachment.sessionExpired")
          : status === 403
            ? say("attachment.noUploadPermission")
            : say("attachment.uploadFailed", { status });
  }
  return new ApiError({ code, message }, status);
}
