import type { PinnedMessage } from "@tdc/types";

/**
 * Pinned messages, which are the room speaking rather than a bookmark.
 *
 * Everybody in the room sees the same set, which is what separates this from
 * the conversations somebody keeps at the top of their own list.
 */
export function usePins(roomId: Ref<string>) {
  const api = useApi();

  const pinned = ref<PinnedMessage[]>([]);
  const pinnedIds = computed(() => new Set(pinned.value.map((item) => item.message.id)));

  async function refresh() {
    try {
      pinned.value = await api.get<PinnedMessage[]>(`/api/rooms/${roomId.value}/pins`);
    } catch {
      pinned.value = [];
    }
  }

  async function pin(eventId: string) {
    await api.put(`/api/rooms/${roomId.value}/messages/${encodeURIComponent(eventId)}/pin`);
    await refresh();
  }

  async function unpin(eventId: string) {
    await api.delete(`/api/rooms/${roomId.value}/messages/${encodeURIComponent(eventId)}/pin`);
    await refresh();
  }

  return { pinned, pinnedIds, refresh, pin, unpin };
}

/**
 * The conversations somebody keeps at the top of their own list.
 *
 * A bookmark rather than an announcement: this is private to the person, and
 * says nothing to anybody else in the room.
 */
export function usePinnedRooms() {
  const api = useApi();

  const ids = ref<string[]>([]);
  const isPinned = computed(() => new Set(ids.value));

  async function refresh() {
    try {
      ids.value = await api.get<string[]>("/api/me/pins");
    } catch {
      ids.value = [];
    }
  }

  async function toggle(roomId: string) {
    if (isPinned.value.has(roomId)) {
      await api.delete(`/api/me/pins/${roomId}`);
    } else {
      await api.put(`/api/me/pins/${roomId}`, { position: ids.value.length });
    }
    await refresh();
  }

  return { ids, isPinned, refresh, toggle };
}
