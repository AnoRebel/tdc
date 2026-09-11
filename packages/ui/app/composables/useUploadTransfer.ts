import type { Attachment } from "@tdc/types";

/**
 * Sending one file's bytes to the server, with progress, as the two clients do
 * it differently.
 *
 * The bookkeeping around an upload — the list, the retries, what counts as
 * finished — is the same everywhere and lives in useUploads. What differs is
 * the transfer itself: the web client sends over XMLHttpRequest, which is the
 * one API that reports how much of a request body has gone out, with the
 * session cookie the browser carries. The desktop client has no cookie in its
 * webview and sends through Go instead. So only the transfer is injected, and
 * the composable above it is shared whole.
 */

/** What a transfer reports back as it runs and when it ends. */
export interface TransferHandlers {
  /** Called as bytes leave, with how many have. */
  onProgress(transferred: number): void;
  /** The attachment the server created, on success. */
  onDone(attachment: Attachment): void;
  /** A message fit to show, on any failure that is not a cancellation. */
  onError(message: string): void;
  /** The person, or the page going away, stopped it. */
  onCancel(): void;
}

/**
 * A running transfer, which can be stopped.
 *
 * Abort is idempotent and safe after the transfer has already ended: useUploads
 * calls it on cleanup without tracking whether each one is still in flight.
 */
export interface Transfer {
  abort(): void;
}

/**
 * Starts sending one file and returns a handle to stop it.
 *
 * roomId and file are what to send; the handlers are how the shared composable
 * hears about it. Deliberately not a Promise: an upload reports progress over
 * time, and a Promise has only one result — the handlers carry the running
 * commentary a progress bar is drawn from.
 */
export type UploadTransfer = (roomId: string, file: File, handlers: TransferHandlers) => Transfer;

export const uploadTransferKey = Symbol("tdc.uploadTransfer") as InjectionKey<UploadTransfer>;

/**
 * How to transfer an upload, from whichever application is running.
 *
 * Required rather than defaulted, like the request transport: the layer does
 * not open an XMLHttpRequest of its own, so no file here assumes the web
 * client's way of sending.
 */
export function useUploadTransfer(): UploadTransfer {
  const provided = inject(uploadTransferKey, null);
  if (!provided) {
    throw new Error(
      "No upload transfer has been provided. An application extending @tdc/ui " +
        "must provide one under uploadTransferKey — see the web client's media plugin.",
    );
  }
  return provided;
}
