import type { Room, RoomMember } from "@tdc/types";

/**
 * What the person may change about one channel.
 *
 * Asked of the server rather than worked out from `can()`, because a grant may
 * be made for this channel alone and the server-wide permission list does not
 * carry those. Every field false is the ordinary answer.
 */
export interface ChannelAdministration {
  /** False for a direct conversation, a project's, or a channel another server holds. */
  administrable: boolean;
  rename: boolean;
  archive: boolean;
  makePrivate: boolean;
  /** Separate, because making a channel public needs the permission server-wide. */
  makePublic: boolean;
  removeMembers: boolean;
  /** Shared with other servers, which keeps its visibility as it is. */
  federated?: boolean;
}

const nothing: ChannelAdministration = {
  administrable: false,
  rename: false,
  archive: false,
  makePrivate: false,
  makePublic: false,
  removeMembers: false,
};

/**
 * Administering one channel: renaming, archiving, visibility and membership.
 *
 * The server decides every one of these whatever the client shows; what this
 * reads is only so a control is not offered to somebody who would be refused.
 */
export function useChannelAdmin(roomId: Ref<string>) {
  const api = useApi();

  const allowed = ref<ChannelAdministration>({ ...nothing });

  /** Anything at all to offer, which is what decides whether settings appear. */
  const anything = computed(
    () =>
      allowed.value.administrable &&
      (allowed.value.rename ||
        allowed.value.archive ||
        allowed.value.makePrivate ||
        allowed.value.makePublic ||
        allowed.value.removeMembers),
  );

  async function refresh() {
    try {
      allowed.value = await api.get<ChannelAdministration>(
        `/api/rooms/${roomId.value}/administration`,
      );
    } catch {
      // Not a member, or not reachable: nothing is offered, which is the
      // right answer to both.
      allowed.value = { ...nothing };
    }
  }

  function rename(details: { name?: string; topic?: string }): Promise<Room> {
    return api.patch<Room>(`/api/rooms/${roomId.value}`, details);
  }

  function setVisibility(visibility: "public" | "private"): Promise<Room> {
    return api.put<Room>(`/api/rooms/${roomId.value}/visibility`, { visibility });
  }

  function archive(): Promise<Room> {
    return api.post<Room>(`/api/rooms/${roomId.value}/archive`);
  }

  function restore(): Promise<Room> {
    return api.delete<Room>(`/api/rooms/${roomId.value}/archive`);
  }

  function members(): Promise<RoomMember[]> {
    return api.get<RoomMember[]>(`/api/rooms/${roomId.value}/members`);
  }

  function remove(userId: string): Promise<void> {
    return api.delete(`/api/rooms/${roomId.value}/members/${userId}`);
  }

  return { allowed, anything, refresh, rename, setVisibility, archive, restore, members, remove };
}
