import { Events } from "@wailsio/runtime";
import * as About from "#bindings/shell";

import { menuEvents } from "~/lib/menu-events";

/**
 * The application's own commands: what this is, and leaving.
 *
 * Both arrive as events because both can be asked for from two places — the
 * Help menu and the tray — and neither should be answered twice.
 */

/** What the About screen shows, as Go reports it. */
export interface AboutDetails {
  name: string;
  version: string;
  platform: string;
  description: string;
  licence: string;
  homepage: string;
}

export function useAppCommands() {
  const about = useState<AboutDetails | null>("about", () => null);
  const showingAbout = useState("about-showing", () => false);
  const confirmingQuit = useState("quit-confirming", () => false);

  async function openAbout() {
    // Fetched when asked for rather than at startup: most people never open
    // it, and it is a request across the boundary either way.
    about.value = await About.AboutService.About();
    showingAbout.value = true;
  }

  onMounted(() => {
    const stops = [
      Events.On(menuEvents.showAbout, () => void openAbout()),
      /*
       * Quitting asks first, because only the interface knows whether
       * anything is unsent — Go emits the request and waits.
       */
      Events.On(menuEvents.quitRequested, () => {
        confirmingQuit.value = true;
      }),
    ];

    onBeforeUnmount(() => {
      for (const stop of stops) stop();
    });
  });

  /** Ends the application, once somebody has said so. */
  function quit() {
    confirmingQuit.value = false;
    Events.Emit(menuEvents.quitConfirmed);
  }

  return { about, showingAbout, confirmingQuit, openAbout, quit };
}
