import type {
  Message,
  Presence,
  Room,
  RoomMember,
  Thread,
  ThreadSummary,
  Report,
  ScheduledMessage,
  TypingPerson,
  UnreadCounts,
} from "@tdc/types";

/** Reading and writing rooms and their messages. */
export function useRooms() {
  const api = useApi();

  function list(): Promise<Room[]> {
    return api.get<Room[]>("/api/rooms");
  }

  function get(roomId: string): Promise<Room> {
    return api.get<Room>(`/api/rooms/${roomId}`);
  }

  function create(details: {
    slug: string;
    name?: string;
    topic?: string;
    visibility?: "public" | "private";
    /** "disposable" makes a chat whose messages the server removes. */
    kind?: "channel" | "disposable";
    /** How long messages last, in seconds. Required for a disposable chat. */
    messageTtlSeconds?: number;
  }): Promise<Room> {
    return api.post<Room>("/api/rooms", details);
  }

  function join(roomId: string): Promise<Room> {
    return api.post<Room>(`/api/rooms/${roomId}/join`);
  }

  function leave(roomId: string): Promise<void> {
    return api.post(`/api/rooms/${roomId}/leave`);
  }

  function timeline(roomId: string): Promise<Message[]> {
    return api.get<Message[]>(`/api/rooms/${roomId}/timeline`);
  }

  function members(roomId: string): Promise<RoomMember[]> {
    return api.get<RoomMember[]>(`/api/rooms/${roomId}/members`);
  }

  /**
   * Posts a message.
   *
   * The client token makes a retry safe: if the response is lost, sending
   * again with the same token returns the original message rather than
   * posting twice.
   */
  function send(
    roomId: string,
    body: string,
    clientToken: string,
    attachmentIds: string[] = [],
  ): Promise<Message> {
    return api.post<Message>(`/api/rooms/${roomId}/messages`, {
      body,
      clientToken,
      attachmentIds,
    });
  }

  function edit(roomId: string, messageId: string, body: string): Promise<Message> {
    return api.patch<Message>(`/api/rooms/${roomId}/messages/${messageId}`, { body });
  }

  function remove(roomId: string, messageId: string): Promise<Message> {
    return api.delete<Message>(`/api/rooms/${roomId}/messages/${messageId}`);
  }

  /** A thread's root message and its replies. */
  function thread(roomId: string, rootId: string): Promise<Thread> {
    return api.get<Thread>(`/api/rooms/${roomId}/threads/${rootId}`);
  }

  /**
   * Reply counts for every thread in a room, in one request.
   *
   * A timeline needs a count per message to render. Asking per message meant a
   * request each — and each of those returned every reply body in full, only
   * for the client to take its length.
   */
  /**
   * Sends a copy of a message to another conversation.
   *
   * A copy rather than a link: a link would vanish when the original is
   * deleted and be readable only by people who can see where it came from.
   */
  function forward(
    roomId: string,
    eventId: string,
    to: string,
    comment?: string,
  ): Promise<Message> {
    return api.post<Message>(
      `/api/rooms/${roomId}/messages/${encodeURIComponent(eventId)}/forward`,
      { to, comment, clientToken: crypto.randomUUID() },
    );
  }

  /**
   * Presses a button on a message.
   *
   * The press becomes an event in the room, which is how it reaches whoever
   * posted the message. Nothing is returned to act on locally: the answer
   * arrives the same way every other event does, so a client showing the
   * outcome shows what actually happened rather than what it assumed.
   */
  function pressAction(roomId: string, eventId: string, action: string): Promise<Message> {
    return api.post<Message>(
      `/api/rooms/${roomId}/messages/${encodeURIComponent(eventId)}/actions/${encodeURIComponent(action)}`,
    );
  }

  /**
   * Bans somebody from one channel.
   *
   * Confined to that channel: every other membership they hold is untouched,
   * which is what separates a ban from a suspension.
   */
  function banFromChannel(
    roomId: string,
    userId: string,
    reason: string,
    hours?: number,
  ): Promise<void> {
    return api.post(`/api/rooms/${roomId}/bans`, { user: userId, reason, hours });
  }

  /** Files a report for a moderator to look at. */
  function report(filing: {
    room?: string;
    eventId?: string;
    subject?: string;
    reason: string;
    detail?: string;
  }): Promise<Report> {
    return api.post<Report>("/api/reports", filing);
  }

  /** Writes a caption on one uploaded file, before the message is sent. */
  function captionAttachment(attachmentId: string, caption: string): Promise<void> {
    return api.put(`/api/attachments/${attachmentId}/caption`, { caption });
  }

  /** Writes a message now to be sent later. */
  function schedule(
    roomId: string,
    body: string,
    sendAt: Date,
    attachments?: string[],
    threadRoot?: string,
  ): Promise<ScheduledMessage> {
    return api.post<ScheduledMessage>(`/api/rooms/${roomId}/scheduled`, {
      body,
      sendAt: sendAt.toISOString(),
      attachments,
      threadRoot,
    });
  }

  /** Somebody's own outstanding scheduled messages. */
  function scheduled(): Promise<ScheduledMessage[]> {
    return api.get<ScheduledMessage[]>("/api/me/scheduled");
  }

  function cancelScheduled(id: string): Promise<void> {
    return api.delete(`/api/me/scheduled/${id}`);
  }

  function threadSummaries(roomId: string): Promise<Record<string, ThreadSummary>> {
    return api.get<Record<string, ThreadSummary>>(`/api/rooms/${roomId}/threads`);
  }

  function reply(
    roomId: string,
    rootId: string,
    body: string,
    clientToken: string,
  ): Promise<Message> {
    return api.post<Message>(`/api/rooms/${roomId}/threads/${rootId}/replies`, {
      body,
      clientToken,
    });
  }

  /** Opens the conversation with these people, or returns the existing one. */
  function openDirect(userIds: string[]): Promise<Room> {
    return api.post<Room>("/api/direct", { userIds });
  }

  function listDirect(): Promise<Room[]> {
    return api.get<Room[]>("/api/direct");
  }

  /** Records how far the person has read, so unread counts clear. */
  function markRead(roomId: string, sequence: number): Promise<void> {
    return api.post(`/api/rooms/${roomId}/read`, { sequence });
  }

  function unread(): Promise<UnreadCounts> {
    return api.get<UnreadCounts>("/api/me/unread");
  }

  function presence(roomId: string): Promise<Presence[]> {
    return api.get<Presence[]>(`/api/rooms/${roomId}/presence`);
  }

  function typing(roomId: string): Promise<TypingPerson[]> {
    return api.get<TypingPerson[]>(`/api/rooms/${roomId}/typing`);
  }

  /** Announces that the person is composing. The server expires it on its own. */
  function startTyping(roomId: string): Promise<void> {
    return api.post(`/api/rooms/${roomId}/typing`);
  }

  return {
    list,
    get,
    create,
    join,
    leave,
    timeline,
    members,
    send,
    edit,
    remove,
    thread,
    threadSummaries,
    forward,
    pressAction,
    report,
    banFromChannel,
    captionAttachment,
    schedule,
    scheduled,
    cancelScheduled,
    reply,
    openDirect,
    listDirect,
    markRead,
    unread,
    presence,
    typing,
    startTyping,
  };
}
