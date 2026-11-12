/*
 * How the interface behaves, as opposed to how it looks.
 *
 * Three choices somebody makes for themselves, each independent of the others
 * and of the theme:
 *
 *   motion     — whether things move when they change
 *   shortcuts  — whether single keys do things
 *   density    — how much fits on the screen
 *   formatting — whether messages show bold, code and the rest, or the text
 *                exactly as it was typed; and whether the composer offers its
 *                formatting buttons
 *
 * Kept in this browser rather than on the account. Unlike the colours, the
 * server has nowhere to put these: its preferences are separate endpoints with
 * typed columns, and adding one here would mean a migration for what is,
 * honestly, a property of the device somebody is sitting at as much as of the
 * person — a phone on a bus and a desktop at a desk can want different motion.
 */

/** What actually happens, once a preference has been resolved. */
export const MOTION_LEVELS = ["full", "reduced", "off"] as const;
export type MotionLevel = (typeof MOTION_LEVELS)[number];

/**
 * What somebody chose.
 *
 * "system" is a real choice rather than the absence of one, as it is for the
 * theme: it follows the operating system's reduce-motion setting, and keeps
 * following it while the application is open.
 */
export const MOTION_PREFERENCES = ["system", ...MOTION_LEVELS] as const;
export type MotionPreference = (typeof MOTION_PREFERENCES)[number];

export function isMotionPreference(value: unknown): value is MotionPreference {
  return typeof value === "string" && (MOTION_PREFERENCES as readonly string[]).includes(value);
}

/**
 * The motion level that applies.
 *
 * Following the system means reduced rather than off when the system asks for
 * less: an interface with no transitions at all loses the cue that something
 * changed, and a fade gives that cue without moving anything. Somebody who
 * wants nothing at all can say so.
 *
 * Anything unreadable is treated as following the system, so a value written
 * by a later version — or by hand — cannot leave somebody with more motion
 * than their system asks for.
 */
export function resolveMotion(preference: unknown, systemReduces: boolean): MotionLevel {
  if (preference === "full" || preference === "reduced" || preference === "off") {
    return preference;
  }
  return systemReduces ? "reduced" : "full";
}

/** How much fits on the screen. */
export const DENSITIES = ["comfortable", "compact"] as const;
export type Density = (typeof DENSITIES)[number];

export function isDensity(value: unknown): value is Density {
  return typeof value === "string" && (DENSITIES as readonly string[]).includes(value);
}

/*
 * High contrast: Crystal Neuo's shared high-contrast theme, over whichever
 * palette or material is chosen — black ground, white text, a yellow accent,
 * no shadows and no translucency.
 *
 * Off unless somebody turns it on, or asks to follow their device: an
 * operating system set to increase contrast means different things on
 * different systems, and somebody who has used this application for months
 * should not find it repainted because of a setting they made elsewhere.
 */
export const CONTRAST_PREFERENCES = ["standard", "more", "system"] as const;
export type ContrastPreference = (typeof CONTRAST_PREFERENCES)[number];

export function isContrastPreference(value: unknown): value is ContrastPreference {
  return typeof value === "string" && (CONTRAST_PREFERENCES as readonly string[]).includes(value);
}

/** Whether high contrast applies; anything unreadable is the default, off. */
export function resolveContrast(preference: unknown, systemWantsMore: boolean): boolean {
  if (preference === "more") return true;
  if (preference === "system") return systemWantsMore;
  return false;
}

/*
 * Parts of the interface somebody may put away.
 *
 * Each is something a person can do without, not something they need to be
 * told: a hidden sidebar link still leaves its page reachable from the search
 * palette and by address, and hiding typing or online status hides it from
 * this person only — nobody else sees anything different.
 */
export const HIDEABLE = [
  "directory",
  "events",
  "projects",
  "previews",
  "typing",
  "presence",
] as const;
export type Hideable = (typeof HIDEABLE)[number];

export function isHideable(value: unknown): value is Hideable {
  return typeof value === "string" && (HIDEABLE as readonly string[]).includes(value);
}

/** Where each choice is stored, so the script and the composable agree. */
export const MOTION_STORAGE_KEY = "tdc-motion";
export const SHORTCUTS_STORAGE_KEY = "tdc-shortcuts";
export const DENSITY_STORAGE_KEY = "tdc-density";
/** Whether messages show their formatting, or the text exactly as typed. */
export const FORMATTING_STORAGE_KEY = "tdc-formatting";
/** Whether the composer shows its row of formatting buttons. */
export const FORMATTING_TOOLBAR_STORAGE_KEY = "tdc-formatting-toolbar";
export const CONTRAST_STORAGE_KEY = "tdc-contrast";
/** Whether a material's glass is frosted, or drawn solid. */
export const GLASS_STORAGE_KEY = "tdc-glass";
/** The parts of the interface put away, as a list of names. */
export const HIDDEN_STORAGE_KEY = "tdc-hidden";

/**
 * The motion, density, contrast and glass decisions, made before the first paint.
 *
 * For the same reason the theme's is: an attribute set from a component lands
 * after the browser has drawn, and the first thing drawn is the thing most
 * likely to be animating in. Somebody who switched motion off should not watch
 * the application slide into place on every load.
 *
 * Inlined into the document head, so it is a string with nothing to import.
 * If storage throws the attribute is simply not set, and the stylesheet falls
 * back to the operating system's own setting.
 */
export const PREPAINT_INTERFACE_SCRIPT =
  "(function(){try{" +
  "var r=document.documentElement;" +
  "var m=localStorage.getItem('tdc-motion');" +
  "if(m!=='full'&&m!=='reduced'&&m!=='off'){" +
  "m=window.matchMedia&&window.matchMedia('(prefers-reduced-motion: reduce)').matches" +
  "?'reduced':'full';}" +
  "r.setAttribute('data-motion',m);" +
  "if(localStorage.getItem('tdc-density')==='compact')r.setAttribute('data-density','compact');" +
  // After the theme's script, so high contrast can insist on dark.
  "var c=localStorage.getItem('tdc-contrast');" +
  "if(c==='more'||(c==='system'&&window.matchMedia&&" +
  "window.matchMedia('(prefers-contrast: more)').matches)){" +
  "r.setAttribute('data-contrast','more');r.classList.add('dark');r.classList.remove('light');}" +
  "if(localStorage.getItem('tdc-glass')==='false')r.setAttribute('data-glass','off');" +
  "}catch(e){}})();";
