import type { NotificationPreferences, NotificationSetting, Room } from "@tdc/types";

/**
 * How the sidebar arranges conversations.
 *
 * Pure, so the arrangement can be tested without drawing anything: which
 * conversations are pinned, in what order, and which list everything else
 * falls into. A conversation appears exactly once — pinning moves it to the top
 * rather than copying it there, so the same unread count is never shown twice.
 */

export interface SidebarGroups {
  /** What this person keeps at the top, in the order they chose. */
  pinned: Room[];
  channels: Room[];
  direct: Room[];
  /** Channels they can see and have not joined. */
  browse: Room[];
}

export function arrangeSidebar(
  channels: Room[],
  direct: Room[],
  browse: Room[],
  pinnedIds: string[],
): SidebarGroups {
  const byId = new Map<string, Room>();
  for (const room of [...channels, ...direct, ...browse]) byId.set(room.id, room);

  // In the chosen order, and only what is still here: a pin on a conversation
  // somebody has left is a bookmark to nowhere, and is not drawn.
  const pinned = pinnedIds.map((id) => byId.get(id)).filter((room): room is Room => !!room);
  const isPinned = new Set(pinned.map((room) => room.id));
  const unpinned = (rooms: Room[]) => rooms.filter((room) => !isPinned.has(room.id));

  return {
    pinned,
    channels: unpinned(channels),
    direct: unpinned(direct),
    browse: unpinned(browse),
  };
}

/**
 * The pinned order after moving one conversation by a step.
 *
 * A move past either end leaves the order as it was rather than wrapping: a
 * conversation at the top that "moves up" to the bottom is a surprise.
 */
export function movePinned(ids: string[], roomId: string, step: -1 | 1): string[] {
  const from = ids.indexOf(roomId);
  const to = from + step;
  if (from < 0 || to < 0 || to >= ids.length) return ids;

  const moved = [...ids];
  [moved[from], moved[to]] = [moved[to]!, moved[from]!];
  return moved;
}

/**
 * Who else is in a direct conversation, by name.
 *
 * The server names a direct conversation after everybody in it, the person
 * reading included, so the reader's own name is taken out — they know they are
 * there. A conversation with only oneself keeps its full name rather than
 * becoming blank.
 */
export function otherParticipants(room: Pick<Room, "name">, ownName?: string): string[] {
  const names = room.name.split(", ").filter(Boolean);
  const others = names.filter((name) => name !== ownName);
  return others.length ? others : names;
}

/** The setting that applies to a conversation: its own, or the default. */
export function settingFor(
  preferences: NotificationPreferences,
  roomId: string,
): NotificationSetting {
  return preferences.rooms[roomId] ?? preferences.default;
}

/**
 * Whether a conversation's unread count is worth a badge.
 *
 * Not when it is muted: somebody who silenced a conversation asked not to be
 * pulled back to it, and a number in the sidebar is a pull. The messages are
 * still unread and still counted — the conversation shows them when opened.
 */
export function showsUnreadBadge(
  preferences: NotificationPreferences,
  roomId: string,
  unread: number | undefined,
): boolean {
  return !!unread && !settingFor(preferences, roomId).muted;
}
