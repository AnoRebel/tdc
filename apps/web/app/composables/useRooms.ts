import type { Message, Room, RoomMember } from "@tdc/types";

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
  function send(roomId: string, body: string, clientToken: string): Promise<Message> {
    return api.post<Message>(`/api/rooms/${roomId}/messages`, { body, clientToken });
  }

  function edit(roomId: string, messageId: string, body: string): Promise<Message> {
    return api.patch<Message>(`/api/rooms/${roomId}/messages/${messageId}`, { body });
  }

  function remove(roomId: string, messageId: string): Promise<Message> {
    return api.delete<Message>(`/api/rooms/${roomId}/messages/${messageId}`);
  }

  return { list, get, create, join, leave, timeline, members, send, edit, remove };
}
