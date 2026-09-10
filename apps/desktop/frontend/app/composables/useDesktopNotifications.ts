import { Events } from "@wailsio/runtime";
import * as Notify from "#bindings/notify";

import { menuEvents } from "~/lib/menu-events";

/**
 * System notifications, which are the ones that arrive when nobody is looking.
 *
 * The interface has its own — a bell with a count and a panel — and those keep
 * working exactly as they do in a browser. These are the other kind, and the
 * reason for installing an application rather than keeping a tab open.
 *
 * Both are shown for the same message. Somebody who dismissed a banner without
 * reading it has not read it, so a count that left it out would be lying.
 */
export function useDesktopNotifications() {
  const allowed = useState("notifications-allowed", () => false);

  /** Whether this machine will show them, asked fresh rather than assumed. */
  async function refresh() {
    allowed.value = await Notify.Service.MayNotify();
  }

  /**
   * Asks for permission.
   *
   * Called when somebody turns notifications on, not at startup: an
   * application that asks before showing anything is asking about something
   * nobody has an opinion on yet, and the usual answer to that is no — which
   * on some platforms cannot be undone from inside the application.
   */
  async function ask(): Promise<boolean> {
    allowed.value = await Notify.Service.AskToNotify();
    return allowed.value;
  }

  return { allowed, refresh, ask };
}

/**
 * Takes an activated notification to the conversation it was about.
 *
 * Registered once, from the root component. Pressing a notification is
 * somebody saying "show me this", and an application that only came to the
 * front would have understood half of it.
 */
export function useNotificationOpens() {
  const router = useRouter();

  onMounted(() => {
    const stop = Events.On(menuEvents.openConversation, (event: { data?: unknown }) => {
      /*
       * Wails delivers the emitted payload as the event's data, which arrives
       * as an array of the emitted arguments.
       */
      const payload = Array.isArray(event?.data) ? event.data[0] : event?.data;
      const target = payload as { server?: string; room?: string; message?: string } | undefined;

      if (!target?.room) return;

      void router.push({
        path: `/rooms/${target.room}`,
        // The message to scroll to, where the notification was about one.
        query: target.message ? { at: target.message, jump: String(Date.now()) } : {},
      });
    });

    onBeforeUnmount(() => stop());
  });
}
