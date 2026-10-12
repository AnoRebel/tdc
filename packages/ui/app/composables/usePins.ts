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
