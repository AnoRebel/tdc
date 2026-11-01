import href from "../assets/css/materials.css?url";

import { isMaterial } from "#layers/ui/app/lib/theme";

/*
 * The stylesheet for Crystal Neuo's materials and the high-contrast theme.
 *
 * Fetched only when one is wanted: six materials in light and dark, and the
 * faces they set in, are worth a download for somebody who chose one and
 * nothing for everybody else, so they stay out of the first load.
 *
 * Linked by its address rather than imported as a module, which would bring
 * the bundler's own loader into the first load to fetch one stylesheet.
 */

let loading: Promise<void> | undefined;

/** Loads the stylesheet, once, however many times it is asked for. */
export function loadMaterials(): Promise<void> {
  loading ??= new Promise((resolve) => {
    const link = document.createElement("link");
    link.rel = "stylesheet";
    link.href = href;
    // Resolved on failure too: the page then shows the default palette, which
    // is better than a choice that never finishes being made.
    link.onload = link.onerror = () => resolve();
    document.head.append(link);
  });
  return loading;
}

/** Whether the page, as the head script left it, needs the stylesheet. */
export function needsMaterials(root: HTMLElement): boolean {
  return root.hasAttribute("data-contrast") || isMaterial(root.getAttribute("data-theme"));
}
