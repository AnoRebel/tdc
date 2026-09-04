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
}
