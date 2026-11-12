import type { Reaction } from "@tdc/types";

/**
 * Reactions on a room's messages.
 *
 * Held for the whole room rather than per message, and read in one request:
 * a timeline draws every message it has, and a request each would be one
 * request per message.
 */
export function useReactions(roomId: Ref<string>, threadRoot?: Ref<string | undefined>) {
  const api = useApi();

  const byEvent = ref<Record<string, Reaction[]>>({});

  // A thread's replies are not in the channel's timeline, so a thread asks for
  // its own: the root and every reply under it.
  async function refresh() {
    const thread = threadRoot?.value;
    try {
      byEvent.value = await api.get<Record<string, Reaction[]>>(
        `/api/rooms/${roomId.value}/reactions` +
          (thread ? `?thread=${encodeURIComponent(thread)}` : ""),
      );
    } catch {
      // Reactions are decoration on a timeline that has already loaded.
      // Losing them is not worth an error over a working conversation.
    }
  }

  /**
   * Adds or removes a reaction, showing the change before the server confirms.
   *
   * Optimistic because a reaction is the most immediate thing in a chat
   * interface: waiting a round trip to see your own click register makes the
   * whole application feel slow. The change is rolled back if the request
   * fails, so a refusal does not leave a reaction that is not there.
   */
  async function toggle(eventId: string, emoji: string, self: string) {
    const before = byEvent.value[eventId] ?? [];
    const existing = before.find((reaction) => reaction.emoji === emoji);
    const removing = existing?.mine ?? false;

    byEvent.value = {
      ...byEvent.value,
      [eventId]: applyLocally(before, emoji, self, removing),
    };

    try {
      if (removing) {
        await api.delete(
          `/api/rooms/${roomId.value}/messages/${encodeURIComponent(eventId)}` +
            `/reactions?emoji=${encodeURIComponent(emoji)}`,
        );
      } else {
        await api.put(
          `/api/rooms/${roomId.value}/messages/${encodeURIComponent(eventId)}/reactions`,
          { emoji },
        );
      }
    } catch {
      // Put back what was there. Refreshing instead would be a second round
      // trip to undo a change that was never applied on the server.
      byEvent.value = { ...byEvent.value, [eventId]: before };
    }
  }

  /** The same change the server will make, applied to what is on screen. */
  function applyLocally(
    reactions: Reaction[],
    emoji: string,
    self: string,
    removing: boolean,
  ): Reaction[] {
    if (removing) {
      return (
        reactions
          .map((reaction) =>
            reaction.emoji === emoji
              ? {
                  ...reaction,
                  count: reaction.count - 1,
                  users: reaction.users.filter((user) => user !== self),
                  mine: false,
                }
              : reaction,
          )
          // An emoji nobody has left is not shown at all, rather than shown as
          // zero.
          .filter((reaction) => reaction.count > 0)
      );
    }

    const existing = reactions.find((reaction) => reaction.emoji === emoji);
    if (!existing) {
      return [...reactions, { emoji, count: 1, users: [self], mine: true }];
    }

    return reactions.map((reaction) =>
      reaction.emoji === emoji
        ? {
            ...reaction,
            count: reaction.count + 1,
            users: [...reaction.users, self],
            mine: true,
          }
        : reaction,
    );
  }

  function forEvent(eventId: string): Reaction[] {
    return byEvent.value[eventId] ?? [];
  }

  return { byEvent, refresh, toggle, forEvent };
}
