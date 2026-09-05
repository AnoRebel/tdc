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
}

/** Someone in a room. */
export interface RoomMember {
  id: string;
  username: string;
  displayName: string;
  address: string;
  joinedAt: string;
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
}

/** The payload of an event, by type. */
export interface MessageContent {
  /** For message.sent and message.edited. */
  body?: string;
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
export type LiveFrameType = "event" | "ready" | "subscribed" | "resync" | "error" | "pong";

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
}
