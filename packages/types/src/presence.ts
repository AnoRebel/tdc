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
  /** Whether one of their automatic replies would answer right now. */
  automaticReply?: boolean;
}

/**
 * An automatic reply: what the server says on somebody's behalf while they
 * are away, to people who write to them directly or name them in a channel.
 */
export interface AutomaticReply {
  id: string;
  enabled: boolean;
  /** Markdown, at most 500 characters. Answers direct messages. */
  message: string;
  /** What a mention is answered with; empty means the same as `message`. */
  mentionMessage: string;
  /** Status keys that turn it on; empty means whatever the status is. */
  statuses: string[];
  /** ISO instants; absent means open at that end. */
  startsAt?: string;
  endsAt?: string;
  /** The zone the window was chosen in. */
  timeZone: string;
  replyToDirect: boolean;
  replyToGroups: boolean;
  replyToMentions: boolean;
  /** Whether it would answer right now. */
  active: boolean;
  updatedAt: string;
}

/** What a client sends to create or change an automatic reply. */
export type AutomaticReplyInput = Omit<AutomaticReply, "id" | "active" | "updatedAt">;

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
  /** The thread they are replying in, when they are writing a reply. */
  threadRoot?: string;
}

/** Unread counts, keyed by room identifier. */
export type UnreadCounts = Record<string, number>;

/** A thread: the message that began it and the replies under it. */
export interface Thread {
  root: import("./rooms").Message;
  /**
   * Everything in the thread, oldest first: the replies, and the edits,
   * deletions and updates made to them, folded the same way a channel is.
   */
  replies: import("./rooms").Message[];
  /** How far the reader has read this thread, as a room sequence; 0 if never. */
  readSequence: number;
  /** Whether the reader hears about new replies here. */
  following: boolean;
}
