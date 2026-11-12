import type {
  AttachmentLimits,
  Message,
  PressRequest,
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
import { learnNames } from "#layers/ui/app/lib/names";

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

  /**
   * Join a room another server hosts.
   *
   * A separate call because what happens differs: this server asks the room's
   * home server, which applies its own rules and may refuse. A refusal here
   * comes from a server this one does not control, so it is reported rather
   * than interpreted.
   */
  function joinRemote(roomId: string): Promise<{ joined: boolean; member: string }> {
    return api.post(`/api/rooms/${roomId}/remote/join`);
  }

  /**
   * Post into a room another server hosts.
   *
   * Nothing is stored locally on the way out. The home server assigns the
   * message its position and replicates it back, so what appears in the room
   * is what everybody else sees rather than a local copy that might differ
   * from what was actually accepted.
   */
  function sendRemote(
    roomId: string,
    body: string,
    attachmentIds: string[] = [],
  ): Promise<{ id: string; sequence: number }> {
    // Files are uploaded here, where the person has an account, and copied
    // from here by the home server and everybody else in the room.
    return api.post(`/api/rooms/${roomId}/remote/messages`, { body, attachmentIds });
  }

  function leave(roomId: string): Promise<void> {
    return api.post(`/api/rooms/${roomId}/leave`);
  }

  function timeline(roomId: string): Promise<Message[]> {
    return api.get<Message[]>(`/api/rooms/${roomId}/timeline`);
  }

  async function members(roomId: string): Promise<RoomMember[]> {
    const people = await api.get<RoomMember[]>(`/api/rooms/${roomId}/members`);
    // Whoever reacted or read a message is named by address there.
    learnNames(people);
    return people;
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
  function pressAction(
    roomId: string,
    eventId: string,
    action: string,
    // The choice or text a menu or field carries, and the version of the
    // controls the person was looking at — so a press through a view the
    // program has since replaced is refused rather than miscounted.
    press?: PressRequest,
  ): Promise<Message> {
    return api.post<Message>(
      `/api/rooms/${roomId}/messages/${encodeURIComponent(eventId)}/actions/${encodeURIComponent(action)}`,
      press,
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

  /** What may be uploaded, and what the person's files occupy of their quota. */
  function attachmentLimits(): Promise<AttachmentLimits> {
    return api.get<AttachmentLimits>("/api/attachments/limits");
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

  /** Posts a reply in a thread. It takes what a message does, files included. */
  function reply(
    roomId: string,
    rootId: string,
    body: string,
    clientToken: string,
    attachmentIds: string[] = [],
  ): Promise<Message> {
    return api.post<Message>(`/api/rooms/${roomId}/threads/${rootId}/replies`, {
      body,
      clientToken,
      attachmentIds,
    });
  }

  /** Records how far the person has read one thread. */
  function markThreadRead(roomId: string, rootId: string, sequence: number): Promise<void> {
    return api.post(`/api/rooms/${roomId}/threads/${rootId}/read`, { sequence });
  }

  /** Follows a thread, or stops following it. */
  function followThread(roomId: string, rootId: string, following: boolean): Promise<void> {
    const path = `/api/rooms/${roomId}/threads/${rootId}/follow`;
    return following ? api.put(path, {}) : api.delete(path);
  }

  /** Opens the conversation with these people, or returns the existing one. */
  function openDirect(userIds: string[], usernames: string[] = []): Promise<Room> {
    return api.post<Room>("/api/direct", { userIds, usernames });
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

  /**
   * Announces that the person is composing — in a thread, when given its root.
   * The server expires it on its own.
   */
  function startTyping(roomId: string, threadRoot?: string): Promise<void> {
    return api.post(`/api/rooms/${roomId}/typing`, threadRoot ? { threadRoot } : undefined);
  }

  return {
    list,
    get,
    create,
    join,
    joinRemote,
    sendRemote,
    leave,
    timeline,
    members,
    send,
    edit,
    remove,
    thread,
    threadSummaries,
    markThreadRead,
    followThread,
    forward,
    pressAction,
    report,
    banFromChannel,
    captionAttachment,
    attachmentLimits,
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
