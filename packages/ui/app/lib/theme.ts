/*
 * What the interface looks like, and who decides.
 *
 * Three independent choices, which is why they are three values rather than
 * one setting with a combinatorial list of names:
 *
 *   the palette   — which family of colours (default, congrator, …)
 *   light or dark — a property of the room somebody is sitting in
 *   the accent    — the one colour that carries emphasis
 *
 * An operator sets the instance's defaults, and a person may override any of
 * them for themselves. Both are applied here so the two paths cannot disagree.
 */

/**
 * Crystal Neuo's six materials (https://crystal-neuo.anorebel.net).
 *
 * Each is chosen as a palette is, and replaces one, but brings more than
 * colours: its own corners, borders, shadows, type and, for three of them,
 * frosted glass. Their stylesheet is loaded only once one is chosen.
 */
export const MATERIALS = ["refraction", "tactile", "signal", "folio", "bloom", "hybrid"] as const;
export type Material = (typeof MATERIALS)[number];

export function isMaterial(value: unknown): value is Material {
  return typeof value === "string" && (MATERIALS as readonly string[]).includes(value);
}

/** A palette, by the name that selects it. */
export const PALETTES = ["default", "congrator", ...MATERIALS] as const;
export type Palette = (typeof PALETTES)[number];

export function isPalette(value: unknown): value is Palette {
  return typeof value === "string" && (PALETTES as readonly string[]).includes(value);
}

/** Whether somebody wants light, dark, or whatever their system says. */
export const APPEARANCES = ["light", "dark", "auto"] as const;
export type Appearance = (typeof APPEARANCES)[number];

export function isAppearance(value: unknown): value is Appearance {
  return typeof value === "string" && (APPEARANCES as readonly string[]).includes(value);
}

/**
 * Whether a stored light-or-dark choice means dark, given the device.
 *
 * The rule the head script applies, for code that has to decide again without
 * the theme composable: nothing stored, or auto, follows the device.
 */
export function meansDark(stored: unknown, deviceIsDark: boolean): boolean {
  if (stored === "dark") return true;
  if (stored === "light") return false;
  return deviceIsDark;
}

/** Where each choice is stored, so the script and the composable agree. */
export const THEME_STORAGE_KEY = "tdc-theme";
export const PALETTE_STORAGE_KEY = "tdc-palette";
export const ACCENT_STORAGE_KEY = "tdc-accent";
/**
 * The derived pair, stored alongside the chosen colour.
 *
 * Kept rather than recomputed before the first paint: deriving a shade is a
 * search, and the script in the document head has to be small and immediate.
 * The colour somebody chose is stored too, so the field can show what they
 * typed rather than what was derived from it.
 */
export const ACCENT_PAIR_STORAGE_KEY = "tdc-accent-pair";

/**
 * The theme decision, made before the first paint.
 *
 * A string rather than a function, because it is inlined into the document
 * head and runs before any of the application has loaded — there is nothing to
 * import it into. Kept here so the config and the test that checks it read the
 * same source, which is the only way a test of a copied string means anything.
 *
 * The palette treats dark as its default: its rule is `.dark, :root:not(.light)`.
 * So what this sets for a light preference is the class that turns that off.
 * Both are set explicitly regardless, so a change to which way the stylesheet
 * defaults cannot silently change what everybody sees.
 *
 * The palette and the accent are applied here for the same reason as the
 * appearance: a warm interface that paints cool for one frame is a flash
 * exactly as long as it takes the application to load.
 *
 * Wrapped in try/catch because localStorage throws in a private window in some
 * browsers, and a theme preference is not worth a blank page.
 */
export const PREPAINT_THEME_SCRIPT =
  "(function(){try{" +
  "var r=document.documentElement;" +
  "var t=localStorage.getItem('tdc-theme');" +
  "var d=t==='dark'||((!t||t==='auto')&&" +
  "window.matchMedia('(prefers-color-scheme: dark)').matches);" +
  "r.classList.toggle('dark',d);r.classList.toggle('light',!d);" +
  "var p=localStorage.getItem('tdc-palette');" +
  "if(p&&p!=='default')r.setAttribute('data-theme',p);" +
  "var a=localStorage.getItem('tdc-accent-pair');" +
  "if(a){try{var p=JSON.parse(a);" +
  "r.style.setProperty('--accent-light',p.light);" +
  "r.style.setProperty('--accent-dark',p.dark);}catch(e){}}" +
  "}catch(e){}})();";

/*
 * Contrast, computed rather than trusted.
 *
 * An accent is a colour somebody chose because they liked it, which is not the
 * same as a colour their text can be read against. The check belongs wherever
 * one is set — a person picking their own, an operator branding an instance —
 * so it lives here rather than in either.
 */

/** A colour as its sRGB components, each 0–1. */
interface Rgb {
  r: number;
  g: number;
  b: number;
}

/**
 * Reads a CSS colour this application might actually be given.
 *
 * oklch because that is what the palettes are written in, and hex because that
 * is what a colour picker produces. Anything else returns null and is refused
 * rather than guessed at: a colour that cannot be measured cannot be checked.
 */
export function parseColour(value: string): Rgb | null {
  const text = value.trim().toLowerCase();

  const hex = /^#?([0-9a-f]{3}|[0-9a-f]{6})$/.exec(text);
  if (hex?.[1]) {
    const digits =
      hex[1].length === 3
        ? [...hex[1]].map((digit) => digit + digit)
        : [hex[1].slice(0, 2), hex[1].slice(2, 4), hex[1].slice(4, 6)];
    const [r, g, b] = digits.map((pair) => parseInt(pair, 16) / 255);
    return { r: r ?? 0, g: g ?? 0, b: b ?? 0 };
  }

  const oklch = /^oklch\(\s*([\d.]+%?)\s+([\d.]+)\s+([\d.]+)/.exec(text);
  if (oklch?.[1] && oklch[2] && oklch[3]) {
    const lightness = oklch[1].endsWith("%")
      ? Number.parseFloat(oklch[1]) / 100
      : Number.parseFloat(oklch[1]);
    return oklchToRgb(lightness, Number.parseFloat(oklch[2]), Number.parseFloat(oklch[3]));
  }

  return null;
}

/** Oklch to sRGB, by way of the Oklab and linear-RGB steps the spec defines. */
function oklchToRgb(lightness: number, chroma: number, hueDegrees: number): Rgb {
  const hue = (hueDegrees * Math.PI) / 180;
  const a = chroma * Math.cos(hue);
  const b = chroma * Math.sin(hue);

  const l = (lightness + 0.3963377774 * a + 0.2158037573 * b) ** 3;
  const m = (lightness - 0.1055613458 * a - 0.0638541728 * b) ** 3;
  const s = (lightness - 0.0894841775 * a - 1.291485548 * b) ** 3;

  return {
    r: gamma(4.0767416621 * l - 3.3077115913 * m + 0.2309699292 * s),
    g: gamma(-1.2684380046 * l + 2.6097574011 * m - 0.3413193965 * s),
    b: gamma(-0.0041960863 * l - 0.7034186147 * m + 1.707614701 * s),
  };
}

/** Linear light to the sRGB values a display actually shows. */
function gamma(channel: number): number {
  const clamped = Math.max(0, Math.min(1, channel));
  return clamped <= 0.0031308 ? clamped * 12.92 : 1.055 * clamped ** (1 / 2.4) - 0.055;
}

/** Relative luminance, as WCAG defines it. */
function luminance({ r, g, b }: Rgb): number {
  const linear = (channel: number) =>
    channel <= 0.04045 ? channel / 12.92 : ((channel + 0.055) / 1.055) ** 2.4;
  return 0.2126 * linear(r) + 0.7152 * linear(g) + 0.0722 * linear(b);
}

/**
 * The contrast ratio between two colours, 1 to 21.
 *
 * Returns null when either colour cannot be read, so a caller has to decide
 * what to do about an unmeasurable colour rather than receiving a number that
 * looks like a measurement.
 */
export function contrastRatio(one: string, other: string): number | null {
  const first = parseColour(one);
  const second = parseColour(other);
  if (!first || !second) return null;

  const brighter = Math.max(luminance(first), luminance(second));
  const darker = Math.min(luminance(first), luminance(second));
  return (brighter + 0.05) / (darker + 0.05);
}

/** What WCAG AA asks of normal text. */
export const AA_CONTRAST = 4.5;

/**
 * The foregrounds each appearance puts on the accent.
 *
 * Light mode draws near-white text on it and dark mode near-black, which is
 * why one colour cannot serve both: a shade dark enough for white text is too
 * dark for black text, and there is no value in between that clears 4.5:1
 * against either — measured, not assumed.
 */
const FOREGROUNDS = {
  light: "oklch(0.99 0 0)",
  dark: "oklch(0.16 0.01 265)",
} as const;

/** An accent, as each appearance needs it. */
export interface AccentPair {
  light: string;
  dark: string;
}

/**
 * Why an accent was refused. The interface puts it into words somebody can act
 * on, in their own language.
 */
export interface AccentRefusal {
  reason: "unreadable" | "unmeasurable";
  /** The best ratio reached while trying, when it could be measured at all. */
  ratio?: number;
}

/** A colour in oklch, which is where the lightness can be adjusted. */
interface Oklch {
  lightness: number;
  chroma: number;
  hue: number;
}

/**
 * Converts to oklch, so a colour's lightness can be moved without its hue.
 *
 * The inverse of oklchToRgb, by the same steps in reverse. Approximate at the
 * edges of the gamut, which does not matter here: the result is only used to
 * search for a lightness, and every candidate is measured as sRGB afterwards.
 */
function rgbToOklch({ r, g, b }: Rgb): Oklch {
  const linear = (channel: number) =>
    channel <= 0.04045 ? channel / 12.92 : ((channel + 0.055) / 1.055) ** 2.4;

  const [lr, lg, lb] = [linear(r), linear(g), linear(b)];

  const l = Math.cbrt(0.4122214708 * lr + 0.5363325363 * lg + 0.0514459929 * lb);
  const m = Math.cbrt(0.2119034982 * lr + 0.6806995451 * lg + 0.1073969566 * lb);
  const s = Math.cbrt(0.0883024619 * lr + 0.2817188376 * lg + 0.6299787005 * lb);

  const lightness = 0.2104542553 * l + 0.793617785 * m - 0.0040720468 * s;
  const a = 1.9779984951 * l - 2.428592205 * m + 0.4505937099 * s;
  const bb = 0.0259040371 * l + 0.7827717662 * m - 0.808675766 * s;

  const hue = (Math.atan2(bb, a) * 180) / Math.PI;

  return {
    lightness,
    chroma: Math.sqrt(a * a + bb * bb),
    hue: hue < 0 ? hue + 360 : hue,
  };
}

/** The ratio between two colours already parsed. */
function ratioBetween(one: Rgb, other: Rgb): number {
  const brighter = Math.max(luminance(one), luminance(other));
  const darker = Math.min(luminance(one), luminance(other));
  return (brighter + 0.05) / (darker + 0.05);
}

/**
 * Finds the shade of this colour that its appearance can read.
 *
 * The hue and chroma are kept and only the lightness moves, so what comes back
 * is recognisably the colour somebody chose. Light mode wants a darker shade
 * (its text is near-white) and dark mode a lighter one, so each is searched
 * from its own end and the first shade that clears the threshold wins — the
 * nearest to what was asked for rather than the safest.
 */
function shadeFor(base: Oklch, foreground: Rgb, direction: "darker" | "lighter"): string | null {
  const step = 0.01;

  for (let offset = 0; offset <= 1; offset += step) {
    const lightness = direction === "darker" ? base.lightness - offset : base.lightness + offset;
    if (lightness < 0 || lightness > 1) break;

    const candidate = oklchToRgb(lightness, base.chroma, base.hue);
    if (ratioBetween(candidate, foreground) >= AA_CONTRAST) {
      return `oklch(${lightness.toFixed(3)} ${base.chroma.toFixed(3)} ${base.hue.toFixed(1)})`;
    }
  }

  return null;
}

/**
 * Turns one chosen colour into the pair each appearance needs.
 *
 * A single value cannot serve both: light mode draws near-white text on the
 * accent and dark mode near-black, so a shade dark enough for one is too dark
 * for the other. Rather than refusing every colour — which is what a single
 * check does, since no value clears both — the hue is kept and the lightness
 * moved until each appearance has a shade its own text can be read on.
 *
 * Returns a refusal only when no shade of this colour works, which in practice
 * means a colour with so little chroma that adjusting it produces grey.
 */
export function deriveAccent(colour: string): AccentPair | AccentRefusal {
  const parsed = parseColour(colour);
  if (!parsed) {
    return { reason: "unmeasurable" };
  }

  const base = rgbToOklch(parsed);
  const lightForeground = parseColour(FOREGROUNDS.light)!;
  const darkForeground = parseColour(FOREGROUNDS.dark)!;

  // Light mode: near-white text, so the accent has to be dark enough.
  const light = shadeFor(base, lightForeground, "darker");
  // Dark mode: near-black text, so it has to be light enough.
  const dark = shadeFor(base, darkForeground, "lighter");

  if (!light || !dark) {
    return { reason: "unreadable" };
  }

  return { light, dark };
}
