/** What somebody has chosen, rather than what is currently shown. */
export type ThemePreference = "light" | "dark" | "auto";

import { THEME_STORAGE_KEY as STORAGE_KEY } from "~/lib/theme";

/**
 * The interface's light or dark appearance.
 *
 * Three settings rather than two. "Auto" follows the device, which is what
 * most people want and what somebody who has never opened this gets — and it
 * is a real choice rather than the absence of one, since somebody who set dark
 * on a device that is currently light meant dark.
 *
 * The class is applied by a script in the document head before the first
 * paint. This composable changes it afterwards, and must apply the same rule
 * the script does or the theme would shift on the first interaction.
 */
export function useTheme() {
  const preference = useState<ThemePreference>("theme", () => "auto");

  /** Whether dark is what should currently be shown. */
  function resolve(choice: ThemePreference): boolean {
    if (choice === "dark") return true;
    if (choice === "light") return false;

    // Auto: whatever the device says, and dark when it says nothing — which
    // matches the palette, whose dark rule applies unless light is asked for.
    return !import.meta.server && window.matchMedia("(prefers-color-scheme: dark)").matches;
  }

  function apply(choice: ThemePreference) {
    if (import.meta.server) return;

    const dark = resolve(choice);
    const classes = document.documentElement.classList;
    classes.toggle("dark", dark);
    classes.toggle("light", !dark);
  }

  function set(choice: ThemePreference) {
    preference.value = choice;
    apply(choice);

    try {
      localStorage.setItem(STORAGE_KEY, choice);
    } catch {
      // A private window may refuse to store it. The choice still applies for
      // this session, which is better than refusing to change the theme.
    }
  }

  /*
   * Read once on the client, so the composable knows what the pre-paint script
   * already decided rather than starting from a default that disagrees with
   * what is on screen.
   */
  onMounted(() => {
    try {
      const stored = localStorage.getItem(STORAGE_KEY);
      if (stored === "light" || stored === "dark" || stored === "auto") {
        preference.value = stored;
      }
    } catch {
      // Unreadable storage means no stored preference, which is auto.
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

  return { preference, set };
}
