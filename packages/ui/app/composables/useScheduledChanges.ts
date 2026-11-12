import type { Message, ScheduledMessage } from "@tdc/types";

/**
 * Changing scheduled messages: rewording, moving, sending now, cancelling.
 *
 * Apart from reading them, because only the list changes them and the list is
 * loaded when first opened — so none of this is in the first download.
 */
export function useScheduledChanges() {
  const api = useApi();
  const { scheduled } = useScheduled();

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

  return { cancel, sendNow, change };
}
