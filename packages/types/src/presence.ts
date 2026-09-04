/**
 * Presence, status, typing and unread state.
 */

/** Whether someone is currently reachable. */
export type PresenceState = "online" | "offline";

/** What one person's presence looks like to another. */
export interface Presence {
  userId: string;
  state: PresenceState;
  /** The operator-configured status the person has chosen. */
  status: string;
  statusText?: string;
  lastSeenAt: string;
}

/** A status an operator has made available. */
export interface ServerStatus {
  key: string;
  label: string;
  /** Whether holding this status withholds notifications. */
  suppressesNotifications: boolean;
}

/** Someone currently composing a message. */
export interface TypingPerson {
  userId: string;
  displayName: string;
}

/** Unread counts, keyed by room identifier. */
export type UnreadCounts = Record<string, number>;

/** A thread: the message that began it and the replies under it. */
export interface Thread {
  root: import("./rooms").Message;
  replies: import("./rooms").Message[];
}
