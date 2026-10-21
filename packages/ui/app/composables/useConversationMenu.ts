import type { PersonHit, Room } from "@tdc/types";

import type { MenuItem } from "#layers/ui/app/components/ContextMenu.vue";

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

  /** The menu's items, built by code fetched the first time a menu opens. */
  async function itemsFor(room: Room): Promise<MenuItem[]> {
    const { conversationMenuItems } = await import("#layers/ui/app/lib/conversationmenu");
    return conversationMenuItems(room, {
      $ts,
      pinnedIds: pins.ids.value,
      setting: notifications.of(room.id),
      waiting: scheduled.inRoom(room.id).length,
      pin: () => pins.pin(room.id),
      unpin: () => pins.unpin(room.id),
      reorder: pins.reorder,
      setLevel: (level) => notifications.setLevel(room.id, level),
      setMuted: (muted) => notifications.setMuted(room.id, muted),
      addPeople: () => newMessage.open(room.id),
      showScheduled: () => scheduled.open(room.id),
      say,
    });
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
