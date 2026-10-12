import type { Message, ScheduledMessage } from "@tdc/types";

/**
 * Somebody's own messages waiting to be sent.
 *
 * Shared, because three places show them: the sidebar counts them, a
 * conversation says how many of its own are waiting, and the list is where
 * they are changed. Scheduling one anywhere has to show up in all three.
 */
export function useScheduled() {
  const api = useApi();

  const scheduled = useState<ScheduledMessage[]>("scheduled-messages", () => []);
  /** Whether the list is open, from wherever asked, and for which conversation. */
  const showing = useState<{ open: boolean; roomId?: string }>("scheduled.open", () => ({
    open: false,
  }));

  async function refresh() {
    try {
      scheduled.value = await api.get<ScheduledMessage[]>("/api/me/scheduled");
    } catch {
      // Kept as it was; the next refresh will try again.
    }
  }

  function inRoom(roomId: string): ScheduledMessage[] {
    return scheduled.value.filter((item) => item.roomId === roomId);
  }

  /*
   * Removed from the list first, and put back where it was if the server
   * refuses — a message that cannot be cancelled is still waiting, and the
   * list must say so.
   */
  async function withdrawn<T>(id: string, request: () => Promise<T>): Promise<T> {
    const before = scheduled.value;
    scheduled.value = before.filter((item) => item.id !== id);
    try {
      return await request();
    } catch (cause) {
      scheduled.value = before;
      throw cause;
    }
  }

  function cancel(id: string) {
    return withdrawn(id, () => api.delete(`/api/me/scheduled/${id}`));
  }

  /** Sends it now, answering with the message as it arrived. */
  function sendNow(id: string) {
    return withdrawn(id, () => api.post<Message>(`/api/me/scheduled/${id}/send`));
  }

  /** Rewords or moves one, keeping the list in the order it will be sent. */
  async function change(id: string, update: { body?: string; sendAt?: Date }) {
    const before = scheduled.value;
    const optimistic = before.map((item) =>
      item.id === id
        ? {
            ...item,
            ...(update.body !== undefined && { body: update.body }),
            ...(update.sendAt && { sendAt: update.sendAt.getTime() }),
          }
        : item,
    );
    scheduled.value = optimistic.sort((a, b) => a.sendAt - b.sendAt);

    try {
      const changed = await api.patch<ScheduledMessage>(`/api/me/scheduled/${id}`, {
        body: update.body,
        sendAt: update.sendAt?.toISOString(),
      });
      scheduled.value = scheduled.value.map((item) => (item.id === id ? changed : item));
    } catch (cause) {
      scheduled.value = before;
      throw cause;
    }
  }

  function open(roomId?: string) {
    showing.value = { open: true, roomId };
  }

  return { scheduled, showing, refresh, inRoom, cancel, sendNow, change, open };
}
