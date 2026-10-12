/**
 * Presence, status, typing and unread state.
 */

/** Whether someone is currently reachable. */
export type PresenceState = "online" | "offline";

/** What one person's presence looks like to another. */
/**
 * One person's presence, as somebody else sees it.
 *
 * Each detail is absent when that person has chosen not to disclose it, which
 * is different from offline: an absent state is no answer, not "offline".
 * Your own presence always carries all three.
 */
export interface Presence {
  userId: string;
  state?: PresenceState;
  /** The operator-configured status the person has chosen. */
  status?: string;
  statusText?: string;
  lastSeenAt?: string;
}

/** A status an operator has made available. */
export interface ServerStatus {
  key: string;
  label: string;
  /** Whether holding this status withholds notifications. */
  suppressesNotifications: boolean;
}

/**
 * Somebody's own status, as they set it.
 *
 * Not what others see: somebody appearing offline is shown to everybody else
 * with no status at all, and is shown here with both.
 */
export interface OwnStatus {
  status: string;
  statusText: string;
  /** When the status reverts on its own, as an ISO time; absent if it does not. */
  expiresAt?: string;
  /** Whether this person appears offline while connected. */
  invisible: boolean;
}

/** How much somebody wants to hear about a conversation. */
export type NotificationLevel = "all" | "mentions" | "none";

/** One notification setting: a level, and whether it is silenced outright. */
export interface NotificationSetting {
  level: NotificationLevel;
  muted: boolean;
}

/** Somebody's default, and each conversation they have set apart from it. */
export interface NotificationPreferences {
  default: NotificationSetting;
  /** Only the conversations with a setting of their own. */
  rooms: Record<string, NotificationSetting>;
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
