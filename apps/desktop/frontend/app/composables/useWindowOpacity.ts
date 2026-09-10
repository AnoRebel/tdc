import * as Shell from "#bindings/shell";

/**
 * How solid the window is.
 *
 * Only the desktop client has this: a browser tab cannot show what is behind
 * it. Somebody who likes seeing a little of their desktop through the
 * application can; somebody who finds that unreadable is unaffected, because
 * the default is solid.
 *
 * The floor is not arbitrary. Below about half, text on any background becomes
 * a struggle whatever the theme — so the slider stops there rather than
 * offering a setting that makes the application unusable.
 */
export function useWindowOpacity() {
  const opacity = useState("window-opacity", () => 100);

  async function refresh() {
    opacity.value = await Shell.TranslucencyService.WindowOpacity();
  }

  /**
   * Paints the page's ground at the same alpha the window has.
   *
   * The two have to agree: the window's transparency is fixed when it opens,
   * so this reads what it actually is rather than what the slider currently
   * shows. A page painted more solid than its window hides the transparency;
   * one painted less solid shows a double-faded ground.
   */
  async function matchTheWindow() {
    const current = await Shell.TranslucencyService.WindowOpacity();
    document.documentElement.style.setProperty("--app-alpha", `${current}%`);
  }

  /**
   * Records the choice.
   *
   * Deliberately does not repaint the page to match: the window's own
   * transparency cannot change until it is reopened, and a page that went
   * see-through against a still-solid window would show a faded ground with
   * nothing behind it — which looks like a bug rather than a preview.
   */
  async function set(percent: number) {
    opacity.value = percent;
    await Shell.TranslucencyService.SetWindowOpacity(percent);
  }

  return { opacity, refresh, set, matchTheWindow };
}
