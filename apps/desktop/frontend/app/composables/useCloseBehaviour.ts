import { Events } from "@wailsio/runtime";

import { menuEvents } from "~/lib/menu-events";

/**
 * What closing the window does.
 *
 * Two reasonable answers, and which one somebody wants is not knowable: for a
 * chat client left open all day, closing means "off my screen"; for somebody
 * who opens it to read something, it means done. So the first close asks, and
 * every one after does what was chosen.
 *
 * The answer is kept on the Go side, which is where the decision has to be
 * made — while the window is closing, which is too late to ask across a
 * boundary and wait.
 */
export function useCloseBehaviour() {
  const asking = useState("close-behaviour-asking", () => false);

  onMounted(() => {
    const stop = Events.On(menuEvents.askCloseBehaviour, () => {
      asking.value = true;
    });

    onBeforeUnmount(() => stop());
  });

  /** Answers, and remembers so it is not asked again. */
  function choose(behaviour: "tray" | "quit") {
    asking.value = false;

    Events.Emit(menuEvents.setCloseBehaviour, { behaviour });

    try {
      localStorage.setItem("tdc-close-behaviour", behaviour);
    } catch {
      // Unreadable storage means being asked again next launch, which is a
      // small cost and better than not applying the choice at all.
    }
  }

  /*
   * Told to Go on startup, so a choice made last time still applies. The Go
   * side holds it for the life of the process and has no memory of its own.
   */
  onMounted(() => {
    try {
      const stored = localStorage.getItem("tdc-close-behaviour");
      if (stored === "tray" || stored === "quit") {
        Events.Emit(menuEvents.setCloseBehaviour, { behaviour: stored });
      }
    } catch {
      // Nothing stored is the state that asks, which is the right default.
    }
  });

  return { asking, choose };
}
