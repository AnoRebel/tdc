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

  /*
   * Shared, so the sidebar and a conversation's own menu agree: pinning from
   * the header moves the conversation in the sidebar at the same moment.
   */
  const ids = useState<string[]>("pinned-rooms", () => []);
  const isPinned = computed(() => new Set(ids.value));

  async function refresh() {
    try {
      ids.value = await api.get<string[]>("/api/me/pins");
    } catch {
      // Left as it was: a list that could not be read is not a list emptied.
    }
  }

  /*
   * Changed here first and on the server second, so the sidebar moves under
   * the pointer rather than a round trip later — and put back if the server
   * refuses, so what is drawn is never something that did not happen.
   */
  async function optimistically(next: string[], request: () => Promise<unknown>) {
    const before = ids.value;
    ids.value = next;
    try {
      await request();
    } catch (cause) {
      ids.value = before;
      throw cause;
    }
  }

  function pin(roomId: string) {
    if (isPinned.value.has(roomId)) return Promise.resolve();
    const position = ids.value.length;
    return optimistically([...ids.value, roomId], () =>
      api.put(`/api/me/pins/${roomId}`, { position }),
    );
  }

  function unpin(roomId: string) {
    return optimistically(
      ids.value.filter((id) => id !== roomId),
      () => api.delete(`/api/me/pins/${roomId}`),
    );
  }

  function toggle(roomId: string) {
    return isPinned.value.has(roomId) ? unpin(roomId) : pin(roomId);
  }

  /*
   * A new order, written as positions. Every pin is rewritten because a
   * position means something only relative to the others; the list is a
   * handful of conversations, so that is a handful of small requests.
   */
  function reorder(next: string[]) {
    return optimistically(next, () =>
      Promise.all(next.map((id, position) => api.put(`/api/me/pins/${id}`, { position }))),
    );
  }

  return { ids, isPinned, refresh, pin, unpin, toggle, reorder };
}
