import { Events } from "@wailsio/runtime";
import { menuEvents, type MenuEvent } from "~/lib/menu-events";

/**
 * What the application menu asks the interface to do.
 *
 * The menu itself performs only what the operating system owns — closing a
 * window, quitting, the clipboard. Everything else it announces, and this is
 * where the announcement becomes an action. Keeping the decision here rather
 * than in Go means the menu and the keyboard shortcut do exactly the same
 * thing, because they call the same function.
 */

/** What a menu item can ask for. */
export interface MenuCommands {
  search?: () => void;
  preferences?: () => void;
  newChannel?: () => void;
  notifications?: () => void;
  keyboardHelp?: () => void;
}

/**
 * Listens while the calling component is mounted.
 *
 * Unsubscribed on unmount, which matters more here than for most listeners:
 * these are registered outside the page, so a handler left behind would keep a
 * reference to a component that no longer exists and act on a screen nobody is
 * looking at.
 */
export function useMenuCommands(commands: MenuCommands): void {
  const handlers: Partial<Record<MenuEvent, (() => void) | undefined>> = {
    [menuEvents.search]: commands.search,
    [menuEvents.preferences]: commands.preferences,
    [menuEvents.newChannel]: commands.newChannel,
    [menuEvents.notifications]: commands.notifications,
    [menuEvents.keyboardHelp]: commands.keyboardHelp,
  };

  onMounted(() => {
    const stops: Array<() => void> = [];

    for (const [event, handler] of Object.entries(handlers)) {
      // A menu item with nothing to do on this screen is left unhandled
      // rather than given an empty function, so the menu can disable it later
      // by asking what is listening.
      if (!handler) continue;
      stops.push(Events.On(event, () => handler()));
    }

    onBeforeUnmount(() => {
      for (const stop of stops) stop();
    });
  });
}
