/**
 * Direct transfers: one person's device sending a file straight to another's.
 *
 * The server records the offer and relays the two devices' negotiation; the
 * file itself goes between them and never rests on the server.
 */

export type DirectTransferState =
  "offered" | "accepted" | "declined" | "cancelled" | "finished" | "failed" | "expired";

/** An offer, as either of its two people sees it. */
export interface DirectTransfer {
  id: string;
  roomId: string;
  senderId: string;
  /** Who is sending, to show the recipient. */
  sender: string;
  recipientId: string;
  /** What the sender says the file is. A claim, shown as one. */
  filename: string;
  size: number;
  contentType?: string;
  state: DirectTransferState;
  /** When an unanswered offer lapses, or an accepted one must have finished. */
  expiresAt: string;
  /** Whether anybody is there to accept, as the sender is told. */
  recipientOnline: boolean;
}

/** The other device's part in connecting a transfer. */
export interface DirectTransferSignal {
  transfer: string;
  from: string;
  signal: { description?: RTCSessionDescriptionInit; candidate?: RTCIceCandidateInit | null };
}
