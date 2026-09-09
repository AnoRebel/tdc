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

/** A palette, by the name that selects it. */
export const PALETTES = ["default", "congrator"] as const;
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

/** Where each choice is stored, so the script and the composable agree. */
export const THEME_STORAGE_KEY = "tdc-theme";
export const PALETTE_STORAGE_KEY = "tdc-palette";
export const ACCENT_STORAGE_KEY = "tdc-accent";

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
  "var a=localStorage.getItem('tdc-accent');" +
  "if(a)r.style.setProperty('--primary',a);" +
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

/** Why an accent was refused, in words somebody can act on. */
export interface AccentRefusal {
  reason: "unreadable" | "unmeasurable";
  message: string;
  /** What it managed, when it could be measured at all. */
  ratio?: number;
}

/**
 * Whether text can be read on this accent.
 *
 * Both foregrounds are checked because an accent is used under white text in
 * one appearance and dark text in the other, and an accent that works for one
 * is not thereby fit for the other — the failure would appear only for people
 * who had switched.
 */
export function checkAccent(accent: string, foregrounds: string[]): AccentRefusal | null {
  for (const foreground of foregrounds) {
    const ratio = contrastRatio(accent, foreground);

    if (ratio === null) {
      return {
        reason: "unmeasurable",
        message:
          "That colour could not be read. Use a hex value like #4f46e5, " + "or an oklch() colour.",
      };
    }

    if (ratio < AA_CONTRAST) {
      return {
        reason: "unreadable",
        ratio,
        message:
          `Text on that colour would be hard to read — ${ratio.toFixed(1)}:1, ` +
          `where ${AA_CONTRAST}:1 is the minimum. Try a darker or more saturated shade.`,
      };
    }
  }

  return null;
}
