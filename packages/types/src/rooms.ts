/**
 * Rooms, messages and the live connection.
 */

/** A channel or conversation. */
export interface Room {
  id: string;
  slug: string;
  name: string;
  topic: string;
  kind: "channel" | "direct" | "disposable";
  visibility: "public" | "private";
  /** The federated form, room@server. */
  address: string;
  archived: boolean;
  /**
   * Whether the requester is in this room. A public room is readable without
   * being a member, but posting requires joining.
   */
  member: boolean;
  /**
   * The server that owns this room, when it is not this one.
   *
   * Absent for an ordinary room. Present for a replica of a room hosted
   * elsewhere, which behaves differently in two ways worth telling somebody
   * about: joining and posting go through that server, and if it is
   * unreachable the room is unavailable here even though this server is fine.
   */
  homeServer?: string;
  /**
   * How long messages here are kept, in seconds. Absent when they are kept
   * indefinitely.
   *
   * Disclosed to everybody who can see the channel: a conversation that
   * quietly deletes itself is one people say things in they would not have
   * said knowing.
   */
  retentionSeconds?: number;
  /**
   * Whether this channel sets its own period rather than inheriting the
   * server's. Different things to be told: one is a decision about this
   * conversation, the other about everything.
   */
  retentionFromChannel?: boolean;
}

/**
 * Someone in a room.
 *
 * The optional fields are details this person may have withheld. Absent means
 * withheld, and a client must not substitute a default: showing "offline" for
 * somebody who withheld their presence discloses exactly what they withheld.
 */
export interface RoomMember {
  id: string;
  username: string;
  displayName: string;
  address: string;
  joinedAt: string;

  online?: boolean;
  status?: string;
  statusText?: string;
  lastSeenAt?: string;

  /**
   * Which details this person withholds, sent only to a moderator who asked to
   * moderate and is being shown them. Its presence is what tells the interface
   * it is displaying an exception rather than ordinary information.
   */
  withheld?: string[];
}

/** An event in a room's timeline. */
export interface Message {
  id: string;
  type: string;
  /** The sender's federated address. */
  sender: string;
  /** Position in the room's total order, used to detect a gap. */
  sequence: number;
  content: MessageContent;
  /** Files carried by this message. Absent when it carries none, which is most. */
  attachments?: Attachment[];
  sentAt: string;
  /**
   * True when the content has been removed. The event stays in the timeline
   * so the conversation does not appear never to have contained it.
   */
  redacted: boolean;
  /** When this message will be removed, where it has been given a lifetime. */
  lifetime?: MessageLifetime;
  /**
   * The thread this belongs to, for a reply and for anything done to one.
   * Absent for a message in the channel itself.
   */
  threadRoot?: string;
}

/** The payload of an event, by type. */
/**
 * Something a client will do by itself when a button is pressed, rather than
 * telling the program that posted the message.
 *
 * A fixed, short list. Every addition is a thing every client must implement
 * and a thing a message can ask any reader's client to do, so it is a decision
 * rather than a configuration. A client meeting a capability it does not know
 * shows the message without that button rather than failing to render it.
 */
export type ActionCapability = "open";

/** One button on a message. */
export interface MessageAction {
  /** What a press reports back, so the sender knows which button it was. */
  name: string;
  /** What a reader sees. */
  label: string;
  /**
   * Absent for the ordinary case, where a press becomes an event to whoever
   * posted the message and the client does nothing itself.
   */
  capability?: ActionCapability;
  /** Where `open` goes. Only http and https; the server refuses the rest. */
  url?: string;
  /** Pressable once in total, by anybody. Enforced by the server. */
  once?: boolean;
  /** When it stops being offered. */
  expires?: string;
}

export interface MessageContent {
  /** For message.sent and message.edited. */
  body?: string;
  /** Buttons this message offers. Absent on a message that offers none. */
  actions?: MessageAction[];
  /** For message.edited: the event this replaces. */
  replaces?: string;
  /** For message.deleted: the event whose content was removed. */
  redacts?: string;
  /** For message.deleted: whether a moderator removed someone else's message. */
  byModerator?: boolean;
  /** For room.created and room.renamed. */
  name?: string;
  topic?: string;
  previousName?: string;
  /** For membership events. */
  member?: string;
  invitedBy?: string;
  removedBy?: string;
}

/** Frame types the server sends over the live connection. */
export type LiveFrameType =
  | "event"
  | "ready"
  | "subscribed"
  | "resync"
  | "error"
  | "pong"
  // A program's answer to one person, never part of a room's log.
  | "private"
  // Another participant's call negotiation, and who is in a call.
  | "call.signal"
  | "call.state"
  // Somebody is ringing you; stop ringing; somebody you rang declined.
  | "call.ring"
  | "call.ring_ended"
  | "call.declined"
  | "call.lobby"
  | "call.admitted"
  | "call.turned_away"
  | "call.reaction";

/** One frame from the live connection. */
export interface LiveFrame {
  type: LiveFrameType;
  /** The room's federated address, when the frame concerns one. */
  room?: string;
  /** The room's local identifier, used for subscription. */
  roomId?: string;
  /** The event's position, so a client can detect a gap. */
  sequence?: number;
  payload?: LiveEventPayload | { message: string };
}

/** The body of an event frame. */
export interface LiveEventPayload {
  id: string;
  type: string;
  sender: string;
  content: MessageContent;
  createdAt: number;
  redacted: boolean;
  /**
   * Set when the event is a reply. The channel view uses it to keep replies
   * out of the main timeline, where they belong to their thread instead.
   */
  threadRoot?: string;
  /**
   * Files carried by the message. Sent with the frame rather than fetched
   * afterwards, so a message never appears with its attachments missing until
   * the next reload.
   */
  attachments?: Attachment[];
}

/**
 * A file attached to a message.
 *
 * The content type is what the server detected from the bytes, never what the
 * uploader declared: a client that decides how to display a file should use
 * this rather than the filename's extension.
 */
export interface Attachment {
  id: string;
  filename: string;
  contentType: string;
  /** Size in bytes. */
  size: number;
  /** Where to fetch the file itself. */
  url: string;
  /** Whether a client can be expected to display this inline. */
  renderable: boolean;
  /**
   * What the sender wrote about this file. Per attachment, so several sent
   * together can each say something different.
   */
  caption?: string;
  /**
   * Whether the image moves. An animation has no still preview, because a
   * thumbnail is a frozen first frame and showing it in place of the
   * animation removes the only thing about a GIF that matters.
   */
  animated?: boolean;
  /**
   * Whether this should arrive covered, with the viewer choosing to reveal it.
   *
   * A flag rather than a withheld URL: the file is still fetchable by anybody
   * who may read the room. Obscuring is about not showing somebody something
   * they did not ask to see, not about who may see it.
   */
  sensitive?: boolean;
  /**
   * Who decided it is sensitive. An uploader's own mark is theirs to lift; a
   * moderator's is not, and a client should not offer a control that will be
   * refused.
   */
  sensitiveSource?: "uploader" | "moderator" | "automatic";
  /**
   * How to display it. Decided by the server so every client agrees, and so
   * markdown — which has no magic bytes and is detected as plain text — is
   * recognised once rather than by each client guessing from a filename.
   */
  kind?:
    | "image"
    | "video"
    | "audio"
    | "pdf"
    | "text"
    | "markdown"
    | "code"
    | "archive"
    | "document"
    | "file";
  /**
   * A reduced version, where one exists. Absent for anything that is not an
   * image, and a client then shows the name, size and type instead of a broken
   * image.
   */
  previewUrl?: string;
  /**
   * The original's dimensions, where known. Used to reserve the right space
   * before the image arrives, so a conversation does not jump as it loads.
   */
  width?: number;
  height?: number;
  uploadedBy: string;
  uploadedAt: string;
}

/** What an operator permits to be uploaded. */
/** A reaction, grouped by emoji rather than listed per person. */
export interface Reaction {
  emoji: string;
  count: number;
  /** Everybody who reacted, as addresses, so a client can name them. */
  users: string[];
  /** Whether the reader is among them, so the button can show its state. */
  mine: boolean;
}

/** A message a room has marked as important. */
export interface PinnedMessage {
  message: Message;
  pinnedBy: string;
  pinnedAt: number;
}

/** Something written now and sent later. */
export interface ScheduledMessage {
  id: string;
  roomId: string;
  body: string;
  attachments?: string[];
  threadRoot?: string;
  /** Milliseconds since the epoch. */
  sendAt: number;
}

/**
 * When a message will be removed, where its sender gave it a lifetime.
 *
 * Disclosed while the message is still readable, so nobody is surprised by its
 * removal and somebody who would rather not answer a message on a timer can
 * see that it is on one.
 */
export interface MessageLifetime {
  /**
   * How the lifetime is reckoned.
   *
   * "after_sending" — a fixed time from when it was sent.
   * "once_seen" — as soon as every other participant has read it.
   * "after_seeing" — a window each recipient gets from their own first
   * reading, expiring when the last of those runs out.
   */
  kind: "after_sending" | "once_seen" | "after_seeing";
  /** The duration in seconds, for the kinds that use one. */
  seconds?: number;
  /**
   * When it goes, absent while that is not yet known — a seen-based lifetime
   * has no expiry until people have read the message, and a countdown from
   * nothing would be invented.
   */
  expiresAt?: string;
}

/** Why something was reported. */
export type ReportReason =
  "spam" | "harassment" | "hate" | "violence" | "sexual" | "illegal" | "other";

/** A filed complaint. */
export interface Report {
  id: string;
  roomId?: string;
  eventId?: string;
  subject: string;
  reporter?: string;
  reason: ReportReason;
  detail?: string;
  /** A copy of what was reported, kept so the report survives a deletion. */
  reportedBody?: string;
  status: "open" | "resolved" | "dismissed";
  resolution?: string;
  createdAt: number;
}

export interface AttachmentLimits {
  maxFileSize: number;
  maxMessageSize: number;
  maxPerMessage: number;
  /** Empty means every type not explicitly blocked is permitted. */
  allowedTypes: string[];
  blockedTypes: string[];
}

/**
 * An upload in flight.
 *
 * Progress is reported from the transfer itself rather than simulated, which is
 * why this carries bytes rather than only a percentage: a stalled upload is
 * visible as a number that stops moving.
 */
export interface UploadProgress {
  /** A client-side identifier, so a queued upload can be addressed before the
   * server has given it one. */
  id: string;
  filename: string;
  size: number;
  transferred: number;
  status: "waiting" | "uploading" | "done" | "failed" | "cancelled";
  /** Set when the status is "failed", stating what went wrong. */
  error?: string;
  /** Set once the upload completes. */
  attachment?: Attachment;
  /**
   * True when the client reduced the file before sending it.
   *
   * Recorded per upload rather than read from a setting, so the tray reports
   * what actually happened to this file — the toggle may have been changed
   * since, and a file that could not be reduced was sent whole regardless.
   */
  reduced?: boolean;
  /** The original's size, where it differed, so the saving is visible. */
  originalSize?: number;
}

/**
 * What a link turned out to be.
 *
 * The image is a URL on whatever site was linked, never something this server
 * stored: fetching it here would turn every posted link into an outbound
 * request to an address of the poster's choosing. A client decides under its
 * own policy whether to load it.
 */
export interface LinkPreview {
  /** The address finally fetched, so a shortener resolves to where it led. */
  url: string;
  title?: string;
  description?: string;
  siteName?: string;
  imageUrl?: string;
}

/** How many replies one message has, without fetching them. */
export interface ThreadSummary {
  replyCount: number;
  /** Milliseconds since the epoch, so a client can order by recent activity. */
  lastReplyAt: number;
  /** Replies the reader has not read, counted only in threads they follow. */
  unread: number;
}

/** Why somebody was notified. */
export type NotificationReason =
  | "mention"
  | "direct"
  | "thread"
  | "reaction"
  | "reply"
  | "join"
  | "forward"
  | "pin"
  // An outgoing webhook of yours was turned off after failing repeatedly.
  | "webhook_disabled"
  // Somebody made you responsible for an issue; detail names it.
  | "assigned"
  // Somebody rang you into a call you did not answer.
  | "missed_call";

/** One thing somebody has been told about. */
export interface Notification {
  id: string;
  roomId: string;
  eventId: string;
  reason: NotificationReason;
  /** Who wrote the message this concerns. */
  sender: string;
  /** A short copy of that message. */
  preview: string;
  /**
   * Who caused it. The same as the sender for a message, and different for
   * everything else: a reaction's event is the message reacted to, whose
   * sender is usually the person being notified.
   */
  actor: string;
  /** What the reason needs — the emoji, for a reaction. */
  detail?: string;
  /**
   * How many unread notifications this entry stands for. One means it stands
   * only for itself; more means it summarises a conversation.
   */
  count?: number;
  /**
   * How many different people are behind that count, which separates "five
   * from Amina" from "five people replied".
   */
  actors?: number;
}

/** How often somebody wants a summary of what they missed. */
export type DigestFrequency = "off" | "daily" | "weekly";

/**
 * Somebody's emailed summary setting.
 *
 * The hour and day are in `timeZone`, an IANA name, so "eight in the morning"
 * stays eight in the morning wherever the server is and across a change to
 * summer time.
 */
export interface DigestSetting {
  frequency: DigestFrequency;
  /** 0 to 23, in `timeZone`. */
  hour: number;
  /** The day of a weekly summary, Sunday being 0. */
  weekday: number;
  /** Empty for somebody who has never chosen; a client fills in its own. */
  timeZone: string;
  /** When the next one is due, absent while off. */
  nextAt?: string;
  lastSentAt?: string;
  /** Whether there is a confirmed address a summary may go to. */
  addressConfirmed: boolean;
}

/** One GIF from a third-party library. */
export interface GifResult {
  id: string;
  /** The provider's alt text, which is what makes a result usable by
   * somebody who cannot see it. */
  description: string;
  /** The full-size animation. */
  url: string;
  /** A smaller version for the picker grid, where a dozen full-size
   * animations at once would be megabytes. */
  previewUrl: string;
  width?: number;
  height?: number;
}

/** A page of GIF results, with the provider both libraries require naming. */
export interface GifSearchResults {
  provider: string;
  results: GifResult[];
}

/** Somebody kept out of one channel. */
export interface ChannelBan {
  userId: string;
  username: string;
  bannedBy: string;
  reason: string;
  /** Absent means permanent. */
  expiresAt?: string;
  createdAt: string;
}

/** Somebody the reader has blocked, which only they can see. */
export interface BlockedPerson {
  id: string;
  username: string;
  displayName: string;
  blockedAt: string;
}

/** Who may open a direct conversation with somebody. */
export type DirectMessagePolicy = "anyone" | "shared" | "nobody";

/**
 * What a member list may say about somebody beyond their name.
 *
 * A name and an address are never optional — they are how a message is
 * attributed and a conversation is opened. These three are, and the server
 * omits a withheld one from the response rather than sending it to be hidden.
 */
export interface DisclosureSettings {
  /** Whether others may see them as online or offline at all. */
  showsPresence: boolean;
  /** Their chosen status and the text explaining it, which travel together. */
  showsStatus: boolean;
  /** When they were last here — the most revealing of the three. */
  showsLastSeen: boolean;
}

/** Somebody's own privacy settings, as the settings screen reads them. */
export interface PrivacySettings extends DisclosureSettings {
  directMessages: DirectMessagePolicy;
  /** Who may ring this person, on servers that offer calls. */
  calls: DirectMessagePolicy;
  /** Whether people on connected servers may ring this person. */
  callsFromOtherServers: boolean;
  /** Whether others are shown that this person is writing. */
  sendsTypingIndication: boolean;
}
