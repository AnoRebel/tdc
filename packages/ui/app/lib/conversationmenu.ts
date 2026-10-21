import type {
  NotificationLevel,
  NotificationSetting,
  OwnStatus,
  Room,
  ServerStatus,
} from "@tdc/types";
import {
  ArrowDown,
  ArrowUp,
  BellOff,
  Clock,
  MessageSquareText,
  Pin,
  PinOff,
  Settings,
  UserPlus,
  X,
} from "lucide-vue-next";

import type { MenuItem } from "#layers/ui/app/components/ContextMenu.vue";
import { movePinned } from "#layers/ui/app/lib/sidebar";

/**
 * The menus for a conversation and for the person themselves.
 *
 * Loaded when a menu is first opened rather than with the application: they
 * are a few kilobytes of labels and icons nobody needs until they ask for a
 * menu, and the first download is what everybody waits for.
 */

type Translate = (key: string, params?: Record<string, string | number>) => string;

export interface ConversationMenuNeeds {
  $ts: Translate;
  pinnedIds: string[];
  setting: NotificationSetting;
  waiting: number;
  pin: () => Promise<unknown>;
  unpin: () => Promise<unknown>;
  reorder: (ids: string[]) => Promise<unknown>;
  setLevel: (level: NotificationLevel) => Promise<unknown>;
  setMuted: (muted: boolean) => Promise<unknown>;
  addPeople: () => void;
  showScheduled: () => void;
  say: (text: string) => void;
}

/**
 * What somebody can do to a conversation for themselves.
 *
 * One list for the sidebar's menu and the conversation's own, so the two never
 * offer different things. Every item here is personal — a pin, a mute, a level
 * — and changes nothing anybody else sees.
 */
export function conversationMenuItems(room: Room, needs: ConversationMenuNeeds): MenuItem[] {
  const { $ts, say } = needs;
  const items: MenuItem[] = [];
  const order = needs.pinnedIds;
  const at = order.indexOf(room.id);
  const pinned = at >= 0;

  /** Runs a change, saying so if the server refused and it was put back. */
  function attempt(change: () => Promise<unknown>, failure: string, success?: string) {
    change().then(
      () => success && say(success),
      () => say(failure),
    );
  }
  const pinFailure = $ts("conversation.couldNotPin");

  items.push(
    pinned
      ? {
          label: $ts("conversation.unpin"),
          icon: PinOff,
          action: () => attempt(needs.unpin, pinFailure),
        }
      : { label: $ts("conversation.pin"), icon: Pin, action: () => attempt(needs.pin, pinFailure) },
  );

  if (pinned && at > 0) {
    items.push({
      label: $ts("conversation.moveUp"),
      icon: ArrowUp,
      action: () => attempt(() => needs.reorder(movePinned(order, room.id, -1)), pinFailure),
    });
  }
  if (pinned && at < order.length - 1) {
    items.push({
      label: $ts("conversation.moveDown"),
      icon: ArrowDown,
      action: () => attempt(() => needs.reorder(movePinned(order, room.id, 1)), pinFailure),
    });
  }

  // Only where it is a member: a preference on a channel somebody has not
  // joined would be a preference about messages they do not receive.
  if (room.member) {
    const notifyFailure = $ts("conversation.couldNotChangeNotifications");

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
          checked: needs.setting.level === level,
          section: index === 0 ? $ts("conversation.notifications") : undefined,
          action: () => attempt(() => needs.setLevel(level), notifyFailure),
        });
      });
    }

    items.push({
      label: $ts("conversation.mute"),
      icon: BellOff,
      choice: "check",
      checked: needs.setting.muted,
      separated: room.kind === "direct",
      action: () =>
        attempt(
          () => needs.setMuted(!needs.setting.muted),
          notifyFailure,
          needs.setting.muted ? $ts("conversation.unmuted") : $ts("conversation.muted"),
        ),
    });
  }

  if (room.kind === "direct") {
    items.push({
      label: $ts("conversation.addPeople"),
      icon: UserPlus,
      separated: true,
      action: needs.addPeople,
    });
  }

  if (needs.waiting) {
    items.push({
      label: $ts("scheduled.inThisConversation", { count: needs.waiting }),
      icon: Clock,
      separated: true,
      action: needs.showScheduled,
    });
  }

  return items;
}

export interface UserMenuNeeds {
  $ts: Translate;
  own: OwnStatus;
  statuses: ServerStatus[];
  appearOfflineKey: string;
  choose: (status: string) => void;
  openDialog: () => void;
  clear: () => void;
  openSettings: () => void;
}

/** The person's own menu: how they appear, a note, and the way to settings. */
export function userMenuItems(needs: UserMenuNeeds): MenuItem[] {
  const { $ts, own } = needs;
  const items: MenuItem[] = needs.statuses.map((status, index) => ({
    label: status.label,
    choice: "radio" as const,
    checked:
      status.key === needs.appearOfflineKey
        ? own.invisible
        : !own.invisible && own.status === status.key,
    section: index === 0 ? $ts("status.showAs") : undefined,
    action: () => needs.choose(status.key),
  }));
  items.push({
    label: $ts("status.setNote"),
    icon: MessageSquareText,
    separated: true,
    action: needs.openDialog,
  });
  if (own.status !== "available" || own.statusText || own.invisible) {
    items.push({ label: $ts("status.clearNow"), icon: X, action: needs.clear });
  }
  items.push({
    label: $ts("nav.settings"),
    icon: Settings,
    separated: true,
    action: needs.openSettings,
  });
  return items;
}
