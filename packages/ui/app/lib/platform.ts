/**
 * Whether this is a Mac, for the one thing it changes: which key means
 * "command".
 *
 * `navigator.platform` is deprecated and specified as something a browser may
 * freeze or empty; some WebKit builds already return an empty string for it. An
 * empty string is not a Mac, which happens to be the right answer on Linux and
 * the wrong one on a Mac whose browser has stopped reporting — so the user
 * agent is consulted as well, and either saying Mac is enough.
 *
 * One function rather than the three separate copies this used to be: the
 * shortcut that binds the key and the reference that names it have to agree, or
 * the application documents a key it does not answer to.
 */
export function isMac(): boolean {
  if (typeof navigator === "undefined") return false;

  // The modern replacement, where it exists.
  const modern = (navigator as Navigator & { userAgentData?: { platform?: string } }).userAgentData;
  if (modern?.platform) return modern.platform.toUpperCase().includes("MAC");

  const platform = navigator.platform ?? "";
  if (platform) return /Mac|iP(hone|ad|od)/i.test(platform);

  return /Mac|iP(hone|ad|od)/i.test(navigator.userAgent ?? "");
}

/** The symbol for the command modifier, as a keyboard reference shows it. */
export function commandKeyLabel(): string {
  return isMac() ? "⌘" : "Ctrl";
}
