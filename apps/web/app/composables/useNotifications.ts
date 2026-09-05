import type { Notification } from "@tdc/types";

/**
 * What has happened that somebody should know about.
 *
 * Held server-side rather than only pushed, which is what makes these work
 * across devices: reading on a phone withdraws what a laptop is showing, and a
 * client that was closed finds what it missed on return.
 */
export function useNotifications() {
  const api = useApi();

  const items = ref<Notification[]>([]);
  const unread = ref(0);

  async function refresh() {
    try {
      items.value = await api.get<Notification[]>("/api/me/notifications");
      unread.value = items.value.length;
    } catch {
      // A badge that cannot be read is not worth interrupting anybody over.
    }
  }

  /**
   * Just the count, which is polled far more often than the list.
   *
   * Separate from refresh because a badge is checked constantly and the list
   * only when somebody opens it — fetching every notification to render a
   * number would be the expensive way to draw one digit.
   */
  async function refreshCount() {
    try {
      const { unread: count } = await api.get<{ unread: number }>("/api/me/notifications/count");
      unread.value = count;
    } catch {
      // Left as it was rather than zeroed: showing nothing when something is
      // waiting is worse than showing a number a moment out of date.
    }
  }

  async function markRead(id: string) {
    // Removed before the server confirms, because dismissing something you
    // have just read should not wait on a round trip.
    const before = items.value;
    items.value = items.value.filter((item) => item.id !== id);
    unread.value = Math.max(0, unread.value - 1);

    try {
      await api.post(`/api/me/notifications/${id}/read`);
    } catch {
      items.value = before;
      unread.value = before.length;
    }
  }

  return { items, unread, refresh, refreshCount, markRead };
}

/**
 * How to phrase one notification.
 *
 * Written here rather than on the server so the wording can change without a
 * deployment, and so a client in another language can phrase it its own way
 * from the same reason.
 */
export function describeNotification(notification: Notification): string {
  const who = shortNameOf(notification.actor);

  switch (notification.reason) {
    case "mention":
      return `${who} mentioned you`;
    case "direct":
      return `${who} sent you a message`;
    case "thread":
      return `${who} posted`;
    case "reaction":
      return `${who} reacted ${notification.detail ?? ""}`.trim();
    case "reply":
      return `${who} replied in your thread`;
    case "join":
      return `${who} joined`;
    case "forward":
      return `${who} forwarded your message`;
    case "pin":
      return `${who} pinned your message`;
    default:
      return `${who} did something`;
  }
}

function shortNameOf(address: string): string {
  return address.split("@")[0] ?? address;
}
