import type { Appearance as StoredAppearance } from "@tdc/types";
import {
  ACCENT_PAIR_STORAGE_KEY,
  ACCENT_STORAGE_KEY,
  APPEARANCES,
  PALETTES,
  PALETTE_STORAGE_KEY,
  THEME_STORAGE_KEY,
  deriveAccent,
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

  /**
   * Whether this person has chosen each of the three for themselves.
   *
   * Kept separately from the values above because "chose the default palette"
   * and "chose nothing" have to stay distinguishable: an operator who changes
   * the instance's palette should move everybody who chose nothing, and nobody
   * who chose.
   */
  const chosen = useState("theme-chosen", () => ({
    palette: false,
    mode: false,
    accent: false,
  }));

  const api = useApi();

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

  /**
   * Sets the accent for each appearance, or clears both.
   *
   * Two properties rather than one, read by the stylesheet's own light and
   * dark blocks: an override that set a single --primary would apply the same
   * shade to both and be unreadable in one of them.
   */
  function applyAccent(light: string | null, dark: string | null) {
    const style = document.documentElement.style;

    if (light && dark) {
      style.setProperty("--accent-light", light);
      style.setProperty("--accent-dark", dark);
    } else {
      style.removeProperty("--accent-light");
      style.removeProperty("--accent-dark");
    }
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

  /**
   * Sends the three choices to the server, where there is one to send to.
   *
   * Failures are swallowed deliberately. The choice has already been applied
   * and stored in this browser, so somebody signed out or offline still gets
   * the interface they asked for — and refusing to change the theme because a
   * request failed would be a worse answer than the theme not following them
   * to another client yet.
   */
  async function publish() {
    if (import.meta.server) return;

    try {
      await api.put<StoredAppearance>("/api/me/preferences/appearance", {
        palette: chosen.value.palette ? palette.value : "",
        mode: chosen.value.mode ? preference.value : "",
        accent: chosen.value.accent ? accent.value : "",
      });
    } catch {
      // Not signed in, or nothing reachable. This browser still has it.
    }
  }

  function set(choice: Appearance) {
    preference.value = choice;
    chosen.value = { ...chosen.value, mode: true };
    apply(choice);
    remember(THEME_STORAGE_KEY, choice);
    void publish();
  }

  function setPalette(choice: Palette) {
    palette.value = choice;
    chosen.value = { ...chosen.value, palette: true };

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
    void publish();
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
       * One colour becomes two shades of itself, because a single value cannot
       * be readable in both appearances: light mode draws near-white text on
       * the accent and dark mode near-black, so a shade dark enough for one is
       * too light for the other. The hue is kept, so what appears is
       * recognisably the colour that was asked for.
       */
      const derived = deriveAccent(wanted);
      if ("reason" in derived) return { refused: derived.message };

      accent.value = wanted;
      if (!import.meta.server) applyAccent(derived.light, derived.dark);
      remember(ACCENT_PAIR_STORAGE_KEY, JSON.stringify(derived));
    } else {
      accent.value = "";
      if (!import.meta.server) applyAccent(null, null);
      remember(ACCENT_PAIR_STORAGE_KEY, "");
    }

    chosen.value = { ...chosen.value, accent: Boolean(wanted) };
    remember(ACCENT_STORAGE_KEY, wanted);
    void publish();
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

  /**
   * Takes what the server knows, and applies whichever wins.
   *
   * The order is: what this person chose, then what the operator chose, then
   * what the palette itself carries. Each of the three resolves independently
   * — somebody may have chosen a palette and left the accent to the operator.
   *
   * Called with the operator's branding and, when signed in, this person's own
   * stored choice. Before this ran, the operator's accent was applied by a
   * watcher that cleared `--primary` whenever the operator had set none, which
   * silently wiped a person's own accent every time server information
   * reloaded.
   */
  function settle(operator: { accent?: string; palette?: string }, mine?: StoredAppearance) {
    if (import.meta.server) return;

    const root = document.documentElement;

    /*
     * The palette: this person's stored choice, then a choice they have just
     * made, then the operator's, then whatever is already applied.
     *
     * The second of those is the one that was missing, and it is what made the
     * toggle appear to do nothing. The operator's branding reports a palette
     * even when the operator has chosen none — "default" is a real palette, not
     * an absence — so it satisfied this chain and won. Somebody clicking "Warm"
     * saw the attribute set and then removed again within the same tick, as the
     * next run of the appearance watcher resolved the operator's default over
     * the top of them.
     *
     * `chosen.palette` is the record of a deliberate choice in this session,
     * which has not reached the account yet. It outranks an operator default
     * for the same reason a stored choice does: the operator says what somebody
     * starts with, not what they must keep.
     */
    const chosenHere = chosen.value.palette;
    const wantedPalette = isPalette(mine?.palette)
      ? mine.palette
      : chosenHere
        ? palette.value
        : isPalette(operator.palette)
          ? operator.palette
          : palette.value;
    chosen.value = { ...chosen.value, palette: isPalette(mine?.palette) || chosenHere };
    palette.value = wantedPalette;
    if (wantedPalette === "default") root.removeAttribute("data-theme");
    else root.setAttribute("data-theme", wantedPalette);

    // Light or dark. The operator has no say here: it is a property of the
    // room somebody is sitting in, not of the instance they are using.
    if (isAppearance(mine?.mode)) {
      chosen.value = { ...chosen.value, mode: true };
      preference.value = mine.mode;
      apply(mine.mode);
    }

    /*
     * The accent, where an empty value means "inherit" rather than "clear".
     * Removing the property is only right when neither this person nor the
     * operator has set one — which is what the old watcher got wrong.
     */
    const wantedAccent = mine?.accent || operator.accent || "";
    chosen.value = { ...chosen.value, accent: Boolean(mine?.accent) };
    accent.value = wantedAccent;

    if (wantedAccent) {
      const derived = deriveAccent(wantedAccent);
      // A stored colour that cannot be derived is left unapplied rather than
      // applied badly: the palette's own accent is always readable.
      if ("light" in derived) applyAccent(derived.light, derived.dark);
      else applyAccent(null, null);
    } else {
      applyAccent(null, null);
    }
  }

  return {
    preference,
    set,
    settle,
    palette,
    setPalette,
    palettes: PALETTES,
    appearances: APPEARANCES,
    accent,
    setAccent,
  };
}
