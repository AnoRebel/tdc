import {
  createGlobalState,
  useLocalStorage,
  usePreferredContrast,
  usePreferredDark,
  usePreferredReducedMotion,
} from "@vueuse/core";
import { computed, watchEffect } from "vue";

import {
  CONTRAST_STORAGE_KEY,
  DENSITY_STORAGE_KEY,
  FORMATTING_STORAGE_KEY,
  FORMATTING_TOOLBAR_STORAGE_KEY,
  GLASS_STORAGE_KEY,
  HIDDEN_STORAGE_KEY,
  HIDEABLE,
  MOTION_STORAGE_KEY,
  SHORTCUTS_STORAGE_KEY,
  isContrastPreference,
  isDensity,
  isHideable,
  isMotionPreference,
  resolveContrast,
  resolveMotion,
  type ContrastPreference,
  type Density,
  type Hideable,
  type MotionPreference,
} from "#layers/ui/app/lib/interface";
import { loadMaterials } from "#layers/ui/app/lib/materials";
import { THEME_STORAGE_KEY, meansDark } from "#layers/ui/app/lib/theme";

/**
 * How the interface behaves: motion, keyboard shortcuts, density, formatting,
 * contrast, glass, and which of its parts are shown.
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

  const storedContrast = useLocalStorage<string>(CONTRAST_STORAGE_KEY, "standard", {
    writeDefaults: false,
  });
  // Frosted unless somebody asks for solid: the glass is most of what three
  // of the materials are.
  const glass = useLocalStorage<boolean>(GLASS_STORAGE_KEY, true, { writeDefaults: false });
  const storedHidden = useLocalStorage<string[]>(HIDDEN_STORAGE_KEY, [], {
    writeDefaults: false,
  });

  /** What the operating system asks for, followed while the application is open. */
  const system = usePreferredReducedMotion();
  const systemContrast = usePreferredContrast();
  const systemDark = usePreferredDark();

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

  const contrastPreference = computed<ContrastPreference>({
    get: () => (isContrastPreference(storedContrast.value) ? storedContrast.value : "standard"),
    set: (value) => (storedContrast.value = value),
  });
  const highContrast = computed(() =>
    resolveContrast(storedContrast.value, systemContrast.value === "more"),
  );

  /** What is put away, with anything unreadable left out. */
  const hidden = computed<Hideable[]>(() =>
    Array.isArray(storedHidden.value) ? storedHidden.value.filter(isHideable) : [],
  );

  /** Whether a part of the interface is shown, which it is unless put away. */
  function shows(part: Hideable): boolean {
    return !hidden.value.includes(part);
  }

  function setShown(part: Hideable, shown: boolean) {
    const rest = hidden.value.filter((each) => each !== part);
    storedHidden.value = shown ? rest : [...rest, part];
  }

  watchEffect(() => {
    if (typeof document === "undefined") return;
    const root = document.documentElement;

    root.setAttribute("data-motion", motion.value);

    if (glass.value) root.removeAttribute("data-glass");
    else root.setAttribute("data-glass", "off");

    /*
     * High contrast insists on dark, its only set of colours. Turned off, the
     * page goes back to whatever light or dark was chosen — decided the way the
     * head script decides it, since the theme composable belongs to whichever
     * component is showing it and this belongs to nobody.
     */
    if (highContrast.value) {
      root.setAttribute("data-contrast", "more");
      void loadMaterials();
      root.classList.add("dark");
      root.classList.remove("light");
    } else if (root.hasAttribute("data-contrast")) {
      root.removeAttribute("data-contrast");
      let stored: string | null = null;
      try {
        stored = localStorage.getItem(THEME_STORAGE_KEY);
      } catch {
        // Unreadable storage is no choice, which follows the device.
      }
      const dark = meansDark(stored, systemDark.value);
      root.classList.toggle("dark", dark);
      root.classList.toggle("light", !dark);
    }
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
    contrastPreference,
    highContrast,
    glass,
    hidden,
    /** Every part that can be put away, in the order a list of them shows. */
    parts: HIDEABLE,
    shows,
    setShown,
  };
});
