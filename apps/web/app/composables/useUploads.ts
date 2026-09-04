import type { Attachment, UploadProgress } from "@tdc/types";

/**
 * Uploads with real progress and cancellation.
 *
 * XMLHttpRequest rather than fetch, deliberately: fetch reports progress on the
 * response but not on the request body, so an upload through it can only be
 * shown as an indeterminate spinner. On a phone sending a photograph over
 * mobile data, the difference between "something is happening" and "sixty per
 * cent of four megabytes" is the difference between waiting and giving up.
 *
 * Streams would be the modern answer, but request-body streaming still requires
 * HTTP/2 and is unevenly supported; this works everywhere.
 */
export function useUploads() {
  const config = useRuntimeConfig();
  const base = config.public.apiBase ?? "";

  /** Uploads currently in flight or recently finished, oldest first. */
  const uploads = ref<UploadProgress[]>([]);

  // Kept outside the reactive state: an XMLHttpRequest is not data, and making
  // it reactive would have Vue walk an object full of browser internals.
  const inFlight = new Map<string, XMLHttpRequest>();

  const active = computed(() =>
    uploads.value.filter((upload) => upload.status === "uploading" || upload.status === "waiting"),
  );

  /** Everything that finished successfully and has not been cleared. */
  const completed = computed(() =>
    uploads.value
      .filter((upload) => upload.status === "done" && upload.attachment)
      .map((upload) => upload.attachment as Attachment),
  );

  function find(id: string): UploadProgress | undefined {
    return uploads.value.find((upload) => upload.id === id);
  }

  /**
   * Starts an upload and returns its client-side identifier.
   *
   * Resolves when the transfer finishes, one way or another. A rejection is not
   * thrown: the failure is recorded on the upload's own state, because a
   * failure needs to stay visible next to a retry button rather than vanishing
   * into an unhandled promise.
   */
  function upload(roomId: string, file: File, original?: File): Promise<UploadProgress> {
    const id = crypto.randomUUID();

    const entry: UploadProgress = {
      id,
      filename: file.name,
      size: file.size,
      transferred: 0,
      status: "waiting",
      // Reduced only when the file being sent is genuinely smaller than what
      // was picked: a file that could not be reduced was sent whole, and
      // saying otherwise would be a lie the tray repeats.
      reduced: original !== undefined && original.size > file.size,
      originalSize: original?.size,
    };
    uploads.value.push(entry);

    return new Promise<UploadProgress>((resolve) => {
      const request = new XMLHttpRequest();
      inFlight.set(id, request);

      const form = new FormData();
      form.append("file", file);

      request.upload.addEventListener("progress", (event) => {
        const current = find(id);
        if (!current) return;

        current.status = "uploading";
        // lengthComputable is false when the browser cannot know the total —
        // rare for a plain file, but the guard keeps the bar from jumping to a
        // meaningless value.
        if (event.lengthComputable) {
          current.transferred = event.loaded;
        }
      });

      request.addEventListener("load", () => {
        const current = find(id);
        inFlight.delete(id);
        if (!current) return resolve(entry);

        if (request.status >= 200 && request.status < 300) {
          current.status = "done";
          current.transferred = current.size;
          try {
            current.attachment = JSON.parse(request.responseText) as Attachment;
          } catch {
            current.status = "failed";
            current.error = "The server's reply could not be read.";
          }
        } else {
          current.status = "failed";
          current.error = messageFrom(request);
        }

        resolve(current);
      });

      request.addEventListener("error", () => {
        const current = find(id);
        inFlight.delete(id);
        if (current) {
          current.status = "failed";
          current.error = "The upload could not reach the server.";
        }
        resolve(current ?? entry);
      });

      // Fires on abort() as well as on a browser-initiated cancellation, so
      // cancelling is not reported as a failure.
      request.addEventListener("abort", () => {
        const current = find(id);
        inFlight.delete(id);
        if (current) current.status = "cancelled";
        resolve(current ?? entry);
      });

      request.open("POST", `${base}/api/rooms/${roomId}/attachments`);
      // The session is a cookie, and XMLHttpRequest does not send credentials
      // cross-origin unless told to.
      request.withCredentials = true;
      request.send(form);
    });
  }

  /** Stops an upload in flight. Nothing is attached. */
  function cancel(id: string) {
    inFlight.get(id)?.abort();
  }

  /**
   * Retries a failed upload without the person selecting the file again.
   *
   * The File is kept on the entry for exactly this: re-opening a file picker to
   * recover from a dropped connection is a poor apology.
   */
  function retry(roomId: string, id: string, file: File): Promise<UploadProgress> {
    remove(id);
    return upload(roomId, file);
  }

  /** Forgets an upload, whatever its state. */
  function remove(id: string) {
    inFlight.get(id)?.abort();
    inFlight.delete(id);
    uploads.value = uploads.value.filter((upload) => upload.id !== id);
  }

  /** Clears everything that has finished, leaving what is still in flight. */
  function clearFinished() {
    uploads.value = uploads.value.filter(
      (upload) => upload.status === "uploading" || upload.status === "waiting",
    );
  }

  // An upload in flight when the page goes away would otherwise leave a
  // half-written object behind for the orphan sweep to find.
  onScopeDispose(() => {
    for (const request of inFlight.values()) request.abort();
    inFlight.clear();
  });

  return { uploads, active, completed, upload, cancel, retry, remove, clearFinished };
}

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
