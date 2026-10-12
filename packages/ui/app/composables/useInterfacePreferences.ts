import { createGlobalState, useLocalStorage, usePreferredReducedMotion } from "@vueuse/core";
import { computed, watchEffect } from "vue";

import {
  DENSITY_STORAGE_KEY,
  FORMATTING_STORAGE_KEY,
  FORMATTING_TOOLBAR_STORAGE_KEY,
  MOTION_STORAGE_KEY,
  SHORTCUTS_STORAGE_KEY,
  isDensity,
  isMotionPreference,
  resolveMotion,
  type Density,
  type MotionPreference,
} from "#layers/ui/app/lib/interface";

/**
 * How the interface behaves: motion, keyboard shortcuts, density and formatting.
 *
 * One instance for the whole application rather than one per caller, so the
 * preferences panel, the keyboard and the stylesheet can never disagree about
 * what was chosen. Each choice is stored in this browser as it is made; the
 * document is updated from the same values, so what is stored and what is
 * applied are one thing rather than two that have to be kept in step.
 *
 * The resolved motion level is written to `<html data-motion>`, and every
 * animation in the stylesheet is keyed off that attribute — which is what
 * makes "off" mean off, rather than "off, except for the components that
 * forgot to check".
 */
export const useInterfacePreferences = createGlobalState(() => {
  // Not written until somebody chooses: an absent value is what lets the
  // operating system keep deciding, and a stored "system" would read the same
  // but survive a future change to what the default should be.
  const storedMotion = useLocalStorage<string>(MOTION_STORAGE_KEY, "system", {
    writeDefaults: false,
  });
  const shortcuts = useLocalStorage<boolean>(SHORTCUTS_STORAGE_KEY, true, {
    writeDefaults: false,
  });
  const storedDensity = useLocalStorage<string>(DENSITY_STORAGE_KEY, "comfortable", {
    writeDefaults: false,
  });

  // On by default: formatting is how somebody who typed a code block meant it
  // to be read. Off shows the source, for anybody who would rather.
  const formatting = useLocalStorage<boolean>(FORMATTING_STORAGE_KEY, true, {
    writeDefaults: false,
  });
  // Off by default: the shortcuts and typing the markers cover most of it, and
  // a row of buttons costs a line of the composer on a phone.
  const formattingToolbar = useLocalStorage<boolean>(FORMATTING_TOOLBAR_STORAGE_KEY, false, {
    writeDefaults: false,
  });

  /** What the operating system asks for, followed while the application is open. */
  const system = usePreferredReducedMotion();

  /** What somebody chose, with anything unreadable read as following the system. */
  const motionPreference = computed<MotionPreference>({
    get: () => (isMotionPreference(storedMotion.value) ? storedMotion.value : "system"),
    set: (value) => (storedMotion.value = value),
  });

  /** What actually applies. */
  const motion = computed(() => resolveMotion(storedMotion.value, system.value === "reduce"));

  const density = computed<Density>({
    get: () => (isDensity(storedDensity.value) ? storedDensity.value : "comfortable"),
    set: (value) => (storedDensity.value = value),
  });

  watchEffect(() => {
    if (typeof document === "undefined") return;
    const root = document.documentElement;

    root.setAttribute("data-motion", motion.value);
    // The default is the absence of the attribute, as it is for the palette:
    // the stylesheet's own sizes are the comfortable ones.
    if (density.value === "compact") root.setAttribute("data-density", "compact");
    else root.removeAttribute("data-density");
  });

  return {
    motionPreference,
    motion,
    /** Whether anything should move at all — the question script-driven motion asks. */
    moves: computed(() => motion.value !== "off"),
    /** Whether things may travel across the screen, rather than only fade. */
    travels: computed(() => motion.value === "full"),
    shortcuts,
    density,
    formatting,
    formattingToolbar,
  };
});
