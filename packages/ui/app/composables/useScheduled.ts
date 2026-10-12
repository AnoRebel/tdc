import type { ScheduledMessage } from "@tdc/types";

/**
 * Somebody's own messages waiting to be sent.
 *
 * Shared, because three places show them: the sidebar counts them, a
 * conversation says how many of its own are waiting, and the list is where
 * they are changed. Scheduling one anywhere has to show up in all three.
 * Changing them is useScheduledChanges, which only the list loads.
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

  function open(roomId?: string) {
    showing.value = { open: true, roomId };
  }

  return { scheduled, showing, refresh, inRoom, open };
}
