import { Browser } from "@wailsio/runtime";

/**
 * Links that leave the application open in the system browser.
 *
 * The webview has nowhere to put them: a link with target="_blank" asks for a
 * new window, which the webview silently refuses, and a plain one navigates
 * the window itself away from TDC. So every press on a link to somewhere else
 * — a message's link, a preview, a meeting address, "Add to Google Calendar",
 * a guide's link out — and every window.open is handed to the browser
 * somebody already uses, where it would have opened on the web anyway.
 *
 * One listener for the whole page rather than a change in each component,
 * because links arrive in rendered Markdown the components never see as
 * elements. A press a component already handled (a download through Go, a
 * route change) has been prevented by the time it bubbles here, and is left
 * alone.
 */
export default defineNuxtPlugin(() => {
  const outward = (href: string): string | null => {
    try {
      const url = new URL(href, window.location.href);
      if (url.origin === window.location.origin) return null;
      return ["http:", "https:", "mailto:"].includes(url.protocol) ? url.href : null;
    } catch {
      return null;
    }
  };

  document.addEventListener("click", (event) => {
    if (event.defaultPrevented || event.button !== 0) return;
    const link = (event.target as Element | null)?.closest?.("a[href]");
    if (!(link instanceof HTMLAnchorElement) || link.hasAttribute("download")) return;
    const href = outward(link.href);
    if (!href) return;
    event.preventDefault();
    void Browser.OpenURL(href);
  });

  const open = window.open.bind(window);
  window.open = (target, ...rest) => {
    const href = target ? outward(String(target)) : null;
    if (!href) return open(target, ...rest);
    void Browser.OpenURL(href);
    return null;
  };
});
