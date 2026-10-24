<script setup lang="ts">
import { nameAt } from "#layers/ui/app/lib/names";
import type { Notification } from "@tdc/types";
import {
  AtSign,
  Bell,
  CalendarClock,
  CalendarX,
  CircleUserRound,
  Forward,
  MessageSquare,
  MessageSquareText,
  Pin,
  Smile,
  UserPlus,
} from "lucide-vue-next";
import { Avatar } from "#layers/ui/app/components/ui/avatar";

/**
 * What has happened while you were away.
 *
 * Grouped by nothing and ordered by time, deliberately: a notification list is
 * read newest-first and dismissed as it is read, and grouping would hide the
 * newest thing behind a heading.
 */

const open = defineModel<boolean>({ default: false });

const emit = defineEmits<{ open: [notification: Notification] }>();

const { $t, $ts } = useI18n();

const { items, unread, refresh, markRead, dismissRoom } = useNotifications();

watch(open, (isOpen) => {
  if (isOpen) void refresh();
});

/** An icon per reason, so the list is scannable without reading every line. */
const icons: Record<string, Component> = {
  mention: AtSign,
  direct: MessageSquare,
  thread: MessageSquareText,
  reaction: Smile,
  reply: MessageSquareText,
  join: UserPlus,
  forward: Forward,
  pin: Pin,
  assigned: CircleUserRound,
  event_invite: CalendarClock,
  event_reminder: CalendarClock,
  event_changed: CalendarClock,
  event_promoted: CalendarClock,
  event_cancelled: CalendarX,
};

function iconFor(notification: Notification) {
  return icons[notification.reason] ?? Bell;
}

/*
 * Opens what the notification is about, and dismisses it on the way.
 *
 * A grouped entry dismisses the whole conversation, since that is what it
 * stood for: dismissing only its newest member would leave the group in place
 * with one fewer behind it.
 */
function go(notification: Notification) {
  if ((notification.count ?? 1) > 1) {
    void dismissRoom(notification.roomId);
  } else {
    void markRead(notification.id);
  }

  open.value = false;
  emit("open", notification);
}

function when(notification: Notification): string {
  return describeNotification(notification, $ts);
}
</script>

<template>
  <!-- Anchored to the left edge of the sidebar and allowed to extend past it:
       the panel is wider than the sidebar, so anchoring it to the right cut
       the text off at the window edge. -->
  <div
    v-if="open"
    class="motion-pop-in absolute bottom-full left-2 z-30 origin-bottom-left mb-2 w-80 overflow-hidden rounded-xl border border-border bg-card shadow-xl"
  >
    <header class="flex items-center justify-between border-b border-border px-3 py-2">
      <h2 class="text-xs font-semibold">{{ $t("notification.title") }}</h2>
      <span v-if="unread" class="text-[0.6875rem] text-muted-foreground">
        {{ $t("notification.unreadCount", { count: unread }) }}
      </span>
    </header>

    <div class="max-h-80 overflow-y-auto p-1.5">
      <p v-if="!items.length" class="px-2 py-8 text-center text-xs text-muted-foreground">
        {{ $t("notification.nothingWaiting") }}. {{ $t("notification.upToDate") }}
      </p>

      <button
        v-for="notification in items"
        :key="notification.id"
        type="button"
        class="motion-row flex w-full cursor-pointer items-start gap-2 rounded-lg px-2 py-2 text-left hover:bg-accent"
        @click="go(notification)"
      >
        <span class="relative mt-0.5 shrink-0">
          <Avatar :name="notification.actorName || nameAt(notification.actor)" size="sm" />
          <!-- The reason, on the avatar rather than beside it: the list stays
               one column and the icon is where the eye already is. -->
          <span
            class="absolute -right-1 -bottom-1 grid size-4 place-items-center rounded-full bg-card"
          >
            <component
              :is="iconFor(notification)"
              class="size-3 text-muted-foreground"
              aria-hidden="true"
            />
          </span>
        </span>

        <span class="min-w-0 flex-1">
          <span class="block truncate text-xs font-medium">{{ when(notification) }}</span>
          <span v-if="notification.preview" class="block truncate text-xs text-muted-foreground">
            {{ notification.preview }}
          </span>
        </span>
      </button>
    </div>
  </div>
</template>
