import type { Attachment, UploadProgress } from "@tdc/types";
import type { PieceUploadHandlers } from "#layers/ui/app/lib/pieceUpload";

/** A running upload, which can be stopped. Safe to stop more than once. */
interface Transfer {
  abort(): void;
}

/**
 * Uploads with real progress, cancellation, and resumption.
 *
 * Every file is sent in pieces (see lib/pieceUpload), so a dropped connection
 * costs one piece rather than the file and a proxy with a small body limit is
 * no obstacle. How one piece travels is injected, because the two clients do
 * it differently: the web over XMLHttpRequest with a cookie, the desktop
 * through Go. Progress matters enough to be worth the seam: on a phone sending
 * a photograph over mobile data, "sixty per cent of four megabytes" is the
 * difference between waiting and giving up.
 */
export function useUploads() {
  // How one piece's bytes travel, from whichever application is running.
  const sendPiece = useUploadTransfer();
  const api = useApi();
  const { $ts } = useNuxtApp();

  /*
   * Starts sending a file. The piece loop is loaded the first time somebody
   * sends something, rather than with every page; the handle is returned at
   * once, so a cancellation before it has loaded still cancels.
   */
  function transfer(roomId: string, file: File, handlers: PieceUploadHandlers): Transfer {
    let inner: Transfer | null = null;
    let stopped = false;
    void import("#layers/ui/app/lib/pieceUpload")
      .then(({ sendInPieces }) => {
        if (stopped) return;
        inner = sendInPieces(
          api,
          sendPiece,
          roomId,
          file,
          handlers,
          $ts("attachment.uploadUnreachable"),
        );
      })
      .catch(() => {
        if (!stopped) handlers.onError($ts("attachment.uploadUnreachable"));
      });
    return {
      abort() {
        if (stopped) return;
        stopped = true;
        if (inner) inner.abort();
        else handlers.onCancel();
      },
    };
  }

  /** Uploads currently in flight or recently finished, oldest first. */
  const uploads = ref<UploadProgress[]>([]);

  // Kept outside the reactive state: a transfer handle is not data, and making
  // it reactive would have Vue walk an object full of browser internals.
  const inFlight = new Map<string, Transfer>();

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
      const running = transfer(roomId, file, {
        onProgress(transferred) {
          const current = find(id);
          if (!current) return;
          current.status = "uploading";
          current.transferred = transferred;
        },

        onDone(attachment) {
          const current = find(id);
          inFlight.delete(id);
          if (!current) return resolve(entry);
          current.status = "done";
          current.transferred = current.size;
          current.attachment = attachment;
          resolve(current);
        },

        onError(message) {
          const current = find(id);
          inFlight.delete(id);
          if (current) {
            current.status = "failed";
            current.error = message;
          }
          resolve(current ?? entry);
        },

        onCancel() {
          const current = find(id);
          inFlight.delete(id);
          if (current) current.status = "cancelled";
          resolve(current ?? entry);
        },
      });

      inFlight.set(id, running);
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
