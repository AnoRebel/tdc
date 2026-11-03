import type { Notification } from "@tdc/types";
import { nameAt } from "#layers/ui/app/lib/names";

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

  /**
   * Dismisses everything unread in one conversation.
   *
   * What a grouped entry stands for: clearing the summary has to clear what it
   * summarised, or the group reappears the moment the list is redrawn.
   */
  async function dismissRoom(roomId: string) {
    const before = items.value;
    const dismissed = before.filter((item) => item.roomId === roomId);

    items.value = before.filter((item) => item.roomId !== roomId);
    unread.value = Math.max(
      0,
      unread.value - dismissed.reduce((total, item) => total + (item.count ?? 1), 0),
    );

    try {
      await api.post(`/api/me/notifications/rooms/${roomId}/read`);
    } catch {
      items.value = before;
      void refreshCount();
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

  return { items, unread, refresh, refreshCount, markRead, dismissRoom };
}

/**
 * How to phrase one notification.
 *
 * Written here rather than on the server so the wording can change without a
 * deployment, and so a client in another language can phrase it its own way
 * from the same reason.
 */
export function describeNotification(
  notification: Notification,
  say: (key: string, params?: Record<string, string | number>) => string,
): string {
  // Their display name, sent with the notification; somebody on another server
  // is called by their address.
  const who = notification.actorName || nameAt(notification.actor);
  const detail = notification.detail ?? "";
  const eventName = () => detail || say("notification.reasons.anEvent");

  /*
   * A group is described by what it stands for rather than by its newest
   * member. "Amina and 4 others" says what happened; "Amina mentioned you",
   * shown once for five notifications, does not.
   */
  const count = notification.count ?? 1;
  /*
   * Except an event's. Its invitation, reminders and changes share the
   * event's conversation, and most come from the server rather than a person,
   * so "localhost and 1 other" would name nobody and not the event either.
   * The newest says what matters now — it is starting, it moved — and the
   * count says there is more.
   */
  if (count > 1 && notification.reason.startsWith("event_")) {
    const newest = describeNotification({ ...notification, count: 1 }, say);
    return say("notification.reasons.eventGrouped", { newest, count });
  }
  if (count > 1) {
    const others = (notification.actors ?? 1) - 1;
    const people =
      others > 0
        ? say(others === 1 ? "notification.reasons.andOther" : "notification.reasons.andOthers", {
            who,
            count: others,
          })
        : who;
    return say("notification.reasons.grouped", { people, count });
  }

  switch (notification.reason) {
    case "mention":
      return say("notification.reasons.mention", { who });
    case "direct":
      return say("notification.reasons.direct", { who });
    case "thread":
      return say("notification.reasons.thread", { who });
    case "reaction":
      return say("notification.reasons.reaction", { who, emoji: detail }).trim();
    case "reply":
      return say("notification.reasons.reply", { who });
    case "join":
      return say("notification.reasons.join", { who });
    case "forward":
      return say("notification.reasons.forward", { who });
    case "pin":
      return say("notification.reasons.pin", { who });
    case "webhook_disabled":
      // Said by the server, not a person, so the sender's name is not used.
      return say("notification.reasons.webhookDisabled", { name: detail });
    case "assigned":
      return say("notification.reasons.assigned", { who, issue: detail }).trim();
    case "missed_call":
      return say("notification.reasons.missedCall", { who });
    case "event_invite":
      return say("notification.reasons.eventInvite", { who, event: eventName() });
    case "event_reminder":
      return say("notification.reasons.eventReminder", { event: eventName() });
    case "event_changed":
      return say("notification.reasons.eventChanged", { event: eventName() });
    case "event_cancelled":
      return say("notification.reasons.eventCancelled", { event: eventName() });
    case "event_promoted":
      return say("notification.reasons.eventPromoted", { event: eventName() });
    default:
      return say("notification.reasons.other", { who });
  }
}
