import type { NotificationLevel, NotificationPreferences, NotificationSetting } from "@tdc/types";

import { settingFor } from "#layers/ui/app/lib/sidebar";

/**
 * How much somebody wants to hear about each conversation.
 *
 * Shared state, read once by the layout: the sidebar dims what is muted and the
 * conversation's own menu shows what is set, and both have to change together.
 * The server is what actually decides who is notified; this only shows its
 * answer and asks it to change.
 */
export function useNotificationPreferences() {
  const api = useApi();

  const preferences = useState<NotificationPreferences>("notification-preferences", () => ({
    default: { level: "mentions", muted: false },
    rooms: {},
  }));

  async function refresh() {
    try {
      preferences.value = await api.get<NotificationPreferences>(
        "/api/me/preferences/notifications",
      );
    } catch {
      // Left as it was. Not knowing what is muted is no reason to show
      // everything as unmuted.
    }
  }

  function of(roomId: string): NotificationSetting {
    return settingFor(preferences.value, roomId);
  }

  /*
   * Set on screen first and put back if the server refuses, so a muted
   * conversation dims the moment somebody asks rather than a round trip later.
   */
  async function set(roomId: string, change: Partial<NotificationSetting>) {
    const before = preferences.value;
    const next = { ...of(roomId), ...change };
    preferences.value = { ...before, rooms: { ...before.rooms, [roomId]: next } };

    try {
      await api.put("/api/me/preferences/notifications", { room: roomId, ...next });
    } catch (cause) {
      preferences.value = before;
      throw cause;
    }
  }

  return {
    preferences,
    refresh,
    of,
    setLevel: (roomId: string, level: NotificationLevel) => set(roomId, { level }),
    setMuted: (roomId: string, muted: boolean) => set(roomId, { muted }),
  };
}
