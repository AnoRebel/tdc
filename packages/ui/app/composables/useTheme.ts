import {
  ACCENT_STORAGE_KEY,
  APPEARANCES,
  PALETTES,
  PALETTE_STORAGE_KEY,
  THEME_STORAGE_KEY,
  checkAccent,
  isAppearance,
  isPalette,
  type Appearance,
  type Palette,
} from "#layers/ui/app/lib/theme";

/** What somebody has chosen, rather than what is currently shown. */
export type ThemePreference = Appearance;

/**
 * What the interface looks like.
 *
 * Three independent choices — the palette, light or dark, and the accent —
 * each of which somebody may set without disturbing the others. Somebody who
 * likes the warm palette in dark with their team's green has made three
 * decisions, and a single list of named combinations would have to enumerate
 * their product.
 *
 * Light or dark has three settings rather than two: "auto" follows the device,
 * which is what most people want and what somebody who has never opened this
 * gets — and it is a real choice rather than the absence of one, since
 * somebody who set dark on a device that is currently light meant dark.
 *
 * All of it is applied by a script in the document head before the first
 * paint. This composable changes it afterwards, and must apply the same rules
 * that script does or the interface would shift on the first interaction.
 */
export function useTheme() {
  const preference = useState<Appearance>("theme", () => "auto");
  const palette = useState<Palette>("palette", () => "default");
  /** Empty means the palette's own accent, which is the common case. */
  const accent = useState<string>("accent", () => "");

  /** Whether dark is what should currently be shown. */
  function resolve(choice: Appearance): boolean {
    if (choice === "dark") return true;
    if (choice === "light") return false;

    // Auto: whatever the device says, and dark when it says nothing — which
    // matches the palette, whose dark rule applies unless light is asked for.
    return !import.meta.server && window.matchMedia("(prefers-color-scheme: dark)").matches;
  }

  function apply(choice: Appearance) {
    if (import.meta.server) return;

    const dark = resolve(choice);
    const classes = document.documentElement.classList;
    classes.toggle("dark", dark);
    classes.toggle("light", !dark);
  }

  /** Stores a choice, or gives up quietly when storage refuses. */
  function remember(key: string, value: string) {
    try {
      if (value) localStorage.setItem(key, value);
      else localStorage.removeItem(key);
    } catch {
      // A private window may refuse to store it. The choice still applies for
      // this session, which is better than refusing to change the theme.
    }
  }

  function set(choice: Appearance) {
    preference.value = choice;
    apply(choice);
    remember(THEME_STORAGE_KEY, choice);
  }

  function setPalette(choice: Palette) {
    palette.value = choice;

    if (!import.meta.server) {
      const root = document.documentElement;
      /*
       * The default is the absence of the attribute rather than
       * data-theme="default": the stylesheet's own :root rules are the default
       * palette, and marking it would mean writing them twice.
       */
      if (choice === "default") root.removeAttribute("data-theme");
      else root.setAttribute("data-theme", choice);
    }

    remember(PALETTE_STORAGE_KEY, choice === "default" ? "" : choice);
  }

  /**
   * Sets the accent, or explains why not.
   *
   * Returns the refusal rather than throwing, because an unreadable colour is
   * something somebody chose and can choose differently — not an error in the
   * application. An empty value clears the override and returns the palette's
   * own accent, which is always readable.
   */
  function setAccent(colour: string): { refused?: string } {
    const wanted = colour.trim();

    if (wanted) {
      /*
       * Checked against both foregrounds, because this accent is used under
       * light text in one appearance and dark text in the other. Approving it
       * against only the current one would break for whoever switched.
       */
      const refusal = checkAccent(wanted, ["#ffffff", "#111111"]);
      if (refusal) return { refused: refusal.message };
    }

    accent.value = wanted;

    if (!import.meta.server) {
      const style = document.documentElement.style;
      if (wanted) style.setProperty("--primary", wanted);
      else style.removeProperty("--primary");
    }

    remember(ACCENT_STORAGE_KEY, wanted);
    return {};
  }

  /*
   * Read once on the client, so the composable knows what the pre-paint script
   * already decided rather than starting from a default that disagrees with
   * what is on screen.
   */
  onMounted(() => {
    try {
      const stored = localStorage.getItem(THEME_STORAGE_KEY);
      if (isAppearance(stored)) preference.value = stored;

      const storedPalette = localStorage.getItem(PALETTE_STORAGE_KEY);
      if (isPalette(storedPalette)) palette.value = storedPalette;

      const storedAccent = localStorage.getItem(ACCENT_STORAGE_KEY);
      if (storedAccent) accent.value = storedAccent;
    } catch {
      // Unreadable storage means no stored preference, which is the default.
    }
  });

  /*
   * Somebody on auto follows their device while the application is open, not
   * only when it starts — changing the system theme at dusk should change this
   * too.
   */
  onMounted(() => {
    const query = window.matchMedia("(prefers-color-scheme: dark)");
    const follow = () => {
      if (preference.value === "auto") apply("auto");
    };

    query.addEventListener("change", follow);
    onUnmounted(() => query.removeEventListener("change", follow));
  });

  return {
    preference,
    set,
    palette,
    setPalette,
    palettes: PALETTES,
    appearances: APPEARANCES,
    accent,
    setAccent,
  };
}
