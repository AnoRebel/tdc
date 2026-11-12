import type { ApiClient, Attachment } from "@tdc/types";
import { isApiError } from "@tdc/types";

import type { UploadTransfer } from "#layers/ui/app/composables/useUploadTransfer";

/**
 * Sending a file in pieces, resuming from whatever the server holds.
 *
 * Loaded the first time somebody sends a file rather than with the page: most
 * visits send nothing, and the conversation should not wait for this.
 *
 * A piece that fails because the server could not be reached, was busy, or
 * already had it is retried with a growing wait, after asking the server how
 * much it holds — so a dropped connection on a train costs one piece, not the
 * file. A refusal of the file itself (too large, not allowed) ends it at once.
 */

/** What the server says about an upload in progress. */
export interface UploadState {
  id: string;
  size: number;
  received: number;
  pieces: number;
  pieceBytes: number;
}

export interface PieceUploadHandlers {
  onProgress(transferred: number): void;
  onDone(attachment: Attachment): void;
  onError(message: string): void;
  onCancel(): void;
}

/** Attempts at one piece before giving up: about a minute and a half of waiting. */
const maxAttempts = 7;

/** Whether a failure is worth retrying rather than reporting. */
function retryable(cause: unknown): boolean {
  if (!isApiError(cause)) return true; // Not reached at all.
  return (
    cause.code === "unavailable" ||
    cause.code === "internal" ||
    cause.code === "conflict" ||
    cause.code === "rate-limited"
  );
}

function wait(ms: number, signal: AbortSignal): Promise<void> {
  return new Promise((resolve) => {
    const timer = setTimeout(resolve, ms);
    signal.addEventListener("abort", () => {
      clearTimeout(timer);
      resolve();
    });
  });
}

export function sendInPieces(
  api: ApiClient,
  sendPiece: UploadTransfer,
  roomId: string,
  file: File,
  handlers: PieceUploadHandlers,
  unreachable: string,
): { abort(): void } {
  const controller = new AbortController();
  let uploadId: string | null = null;
  let settled = false;

  const settle = (report: () => void) => {
    if (settled) return;
    settled = true;
    report();
  };
  const abandon = () => {
    if (uploadId) void api.delete(`/api/uploads/${uploadId}`).catch(() => {});
  };

  void (async () => {
    try {
      let state = await api.post<UploadState>(`/api/rooms/${roomId}/uploads`, {
        filename: file.name,
        size: file.size,
      });
      uploadId = state.id;
      // Stopped while the server was answering: nothing to keep.
      if (controller.signal.aborted) return abandon();

      let failures = 0;
      while (state.received < state.size) {
        const start = state.received;
        const end = Math.min(start + state.pieceBytes, state.size);
        try {
          await sendPiece(
            state.id,
            state.pieces,
            file.slice(start, end),
            (sent) => handlers.onProgress(start + sent),
            controller.signal,
          );
          state = { ...state, received: end, pieces: state.pieces + 1 };
          handlers.onProgress(end);
          failures = 0;
        } catch (cause) {
          if (controller.signal.aborted) return;
          failures += 1;
          if (!retryable(cause) || failures >= maxAttempts) throw cause;
          await wait(Math.min(30_000, 500 * 2 ** failures), controller.signal);
          if (controller.signal.aborted) return;
          // Carry on from what the server actually holds: a piece whose
          // answer was lost may well have arrived.
          state = await api.get<UploadState>(`/api/uploads/${state.id}`).catch(() => state);
        }
      }

      const attachment = await api.post<Attachment>(`/api/uploads/${state.id}/finish`);
      settle(() => handlers.onDone(attachment));
    } catch (cause) {
      if (controller.signal.aborted) return;
      settle(() => handlers.onError(isApiError(cause) ? cause.message : unreachable));
    }
  })();

  return {
    abort() {
      if (settled) return;
      settled = true;
      controller.abort();
      abandon();
      handlers.onCancel();
    },
  };
}
