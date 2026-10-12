import type { NotificationLevel, PersonHit, Room } from "@tdc/types";
import { ArrowDown, ArrowUp, BellOff, Clock, Pin, PinOff, UserPlus } from "lucide-vue-next";

import type { MenuItem } from "#layers/ui/app/components/ContextMenu.vue";
import { movePinned } from "#layers/ui/app/lib/sidebar";

/**
 * What somebody can do to a conversation for themselves.
 *
 * One list for the sidebar's menu and the conversation's own, so the two never
 * offer different things. Every item here is personal — a pin, a mute, a level
 * — and changes nothing anybody else sees; what the channel itself allows is in
 * its settings.
 */
export function useConversationMenu() {
  const { $ts } = useI18n();
  const pins = usePinnedRooms();
  const notifications = useNotificationPreferences();
  const scheduled = useScheduled();
  const newMessage = useNewMessage();
  const { say } = useNotice();

  /** Runs a change, saying so if the server refused and it was put back. */
  function attempt(change: () => Promise<unknown>, failure: string, success?: string) {
    change().then(
      () => success && say(success),
      () => say(failure),
    );
  }

  function itemsFor(room: Room): MenuItem[] {
    const items: MenuItem[] = [];
    const pinned = pins.isPinned.value.has(room.id);
    const order = pins.ids.value;
    const at = order.indexOf(room.id);

    items.push(
      pinned
        ? {
            label: $ts("conversation.unpin"),
            icon: PinOff,
            action: () => attempt(() => pins.unpin(room.id), $ts("conversation.couldNotPin")),
          }
        : {
            label: $ts("conversation.pin"),
            icon: Pin,
            action: () => attempt(() => pins.pin(room.id), $ts("conversation.couldNotPin")),
          },
    );

    if (pinned && at > 0) {
      items.push({
        label: $ts("conversation.moveUp"),
        icon: ArrowUp,
        action: () =>
          attempt(
            () => pins.reorder(movePinned(order, room.id, -1)),
            $ts("conversation.couldNotPin"),
          ),
      });
    }
    if (pinned && at >= 0 && at < order.length - 1) {
      items.push({
        label: $ts("conversation.moveDown"),
        icon: ArrowDown,
        action: () =>
          attempt(
            () => pins.reorder(movePinned(order, room.id, 1)),
            $ts("conversation.couldNotPin"),
          ),
      });
    }

    // Only where it is a member: a preference on a channel somebody has not
    // joined would be a preference about messages they do not receive.
    if (room.member) {
      const setting = notifications.of(room.id);

      /*
       * Levels for a channel only. In a direct conversation every message is
       * addressed to the reader, so "mentions" and "everything" are the same
       * thing, and the choice that matters is whether to be told at all.
       */
      if (room.kind !== "direct") {
        const levels: { level: NotificationLevel; label: string }[] = [
          { level: "all", label: $ts("conversation.levelAll") },
          { level: "mentions", label: $ts("conversation.levelMentions") },
          { level: "none", label: $ts("conversation.levelNone") },
        ];
        levels.forEach(({ level, label }, index) => {
          items.push({
            label,
            choice: "radio",
            checked: setting.level === level,
            section: index === 0 ? $ts("conversation.notifications") : undefined,
            action: () =>
              attempt(
                () => notifications.setLevel(room.id, level),
                $ts("conversation.couldNotChangeNotifications"),
              ),
          });
        });
      }

      items.push({
        label: $ts("conversation.mute"),
        icon: BellOff,
        choice: "check",
        checked: setting.muted,
        separated: room.kind === "direct",
        action: () =>
          attempt(
            () => notifications.setMuted(room.id, !setting.muted),
            $ts("conversation.couldNotChangeNotifications"),
            setting.muted ? $ts("conversation.unmuted") : $ts("conversation.muted"),
          ),
      });
    }

    if (room.kind === "direct") {
      items.push({
        label: $ts("conversation.addPeople"),
        icon: UserPlus,
        separated: true,
        action: () => newMessage.open(room.id),
      });
    }

    const waiting = scheduled.inRoom(room.id).length;
    if (waiting) {
      items.push({
        label: $ts("scheduled.inThisConversation", { count: waiting }),
        icon: Clock,
        separated: true,
        action: () => scheduled.open(room.id),
      });
    }

    return items;
  }

  return { itemsFor };
}

/**
 * Whether the new-message dialog is open, and the conversation it is adding to.
 *
 * Shared state, because the sidebar, the search palette and a conversation's
 * own menu all open the same dialog.
 */
export function useNewMessage() {
  const state = useState<{ open: boolean; from?: string; people?: PersonHit[] }>(
    "new-message",
    () => ({ open: false }),
  );

  return {
    state,
    /** Opens it, starting from the people already in a conversation if one is given. */
    open: (from?: string) => {
      state.value = { open: true, from };
    },
    /** Opens it with these people already chosen, for adding more beside them. */
    openWith: (people: PersonHit[]) => {
      state.value = { open: true, people };
    },
  };
}
