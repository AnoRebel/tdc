import type {
  Message,
  Presence,
  Room,
  RoomMember,
  Thread,
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
