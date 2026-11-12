/**
 * Sending one piece of a file to the server, with progress, as the two clients
 * do it differently.
 *
 * Everything else about an upload — declaring it, resuming it from what the
 * server holds, retrying a piece, finishing it — is the same everywhere and
 * goes through the ordinary API client, which both clients already provide.
 * What differs is how the bytes of one piece travel: the web client sends them
 * raw over XMLHttpRequest, the one API that reports how much of a request body
 * has gone out; the desktop client has no session in its webview and sends
 * them through Go, as base64 in JSON. So only that is injected.
 */

/**
 * Sends one piece of an upload and resolves once the server has it.
 *
 * Rejects with an ApiError for a refusal (so a 409 can be told from a 400) and
 * with anything else for a server that could not be reached. The signal stops
 * it; a stopped piece rejects and is not reported as a failure by the caller.
 */
export type UploadTransfer = (
  uploadId: string,
  piece: number,
  data: Blob,
  onProgress: (sent: number) => void,
  signal: AbortSignal,
) => Promise<void>;

export const uploadTransferKey = Symbol("tdc.uploadTransfer") as InjectionKey<UploadTransfer>;

/**
 * How to send a piece, from whichever application is running.
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
