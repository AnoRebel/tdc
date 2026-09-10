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
   * Applied as it moves rather than on release, because the whole point is
   * seeing what a value looks like before settling on it.
   */
  async function set(percent: number) {
    opacity.value = percent;
    await Shell.TranslucencyService.SetWindowOpacity(percent);
  }

  return { opacity, refresh, set };
}
