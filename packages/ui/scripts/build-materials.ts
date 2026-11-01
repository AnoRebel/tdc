#!/usr/bin/env bun
/**
 * Crystal Neuo's six materials, as this interface's own variables.
 *
 *   bun packages/ui/scripts/build-materials.ts [path/to/crystal-neuo/tokens.json]
 *
 * Reads Crystal Neuo's single source of truth (packages/tokens/src/tokens.json
 * in https://github.com/AnoRebel/crystal-neuo, a checkout beside this one by
 * default) and writes app/assets/css/materials.generated.css: for each material,
 * light and dark, the colours, corners, shadows, glass and type this interface
 * draws with, under data-theme="<material>" — the attribute a palette is chosen
 * by — plus the high-contrast theme every material shares, under
 * data-contrast="more".
 *
 * Generated rather than copied by hand so a change to the design system is one
 * command here, and so the mapping from its roles to these variables is written
 * once, below, where it can be read. The output is committed: nothing builds it
 * on the way to a browser.
 *
 * Crystal Neuo checks every text and surface pair it declares for contrast; the
 * roles chosen here are those pairs (ink on canvas, on-accent on accent, ink on
 * accent-soft…), and materials.test.ts measures the result again in this
 * interface's terms.
 */
import { spawnSync } from "node:child_process";
import { readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";

const here = import.meta.dirname;
const source =
  process.argv[2] ?? join(here, "../../../../crystal-neuo/packages/tokens/src/tokens.json");
const output = join(here, "../app/assets/css/materials.generated.css");

type Roles = Record<string, string>;
interface Token {
  value: string | Record<string, string>;
}
interface Tokens {
  meta: { version: string; materials: string[] };
  roles: Record<string, { light: Roles; dark: Roles; extends?: string } & Roles>;
  semantic: Record<string, Record<string, Token>>;
}

const tokens = JSON.parse(readFileSync(source, "utf8")) as Tokens;
const materials = tokens.meta.materials;

/** A material's roles in a theme; Hybrid is Refraction's with its own few on top. */
function roles(material: string, theme: "light" | "dark"): Roles {
  const own = tokens.roles[material]!;
  const base = own.extends ? tokens.roles[own.extends]![theme] : {};
  return { ...base, ...own[theme] };
}

/** A token's value for one material. */
function byMaterial(group: string, name: string, material: string): string {
  const value = tokens.semantic[group]![name]!.value;
  return typeof value === "string" ? value : value[material]!;
}

/** Replaces {role.x} with the colour. */
function fill(value: string, palette: Roles): string {
  return value.replace(/\{role\.([a-z0-9-]+)\}/g, (_, role: string) => {
    const colour = palette[role];
    if (!colour) throw new Error(`no role ${role}`);
    return colour;
  });
}

/** A radius at the web platform's scale, which is 1. */
function radius(name: string, material: string): number {
  const match = /calc\((\d+(?:\.\d+)?)px/.exec(byMaterial("radius", name, material));
  if (!match) throw new Error(`cannot read ${name} for ${material}`);
  return Number(match[1]);
}

/*
 * The faces, by the names the font packages this layer loads give them.
 * Crystal Neuo names the families; the packages add "Variable". Doto and IBM
 * Plex Mono, its numeric faces, are not used: numbers here are timestamps and
 * counts, which read better in the text face.
 */
const FACES: Record<string, string> = {
  Geist: '"Geist Variable"',
  "Bricolage Grotesque": '"Bricolage Grotesque Variable"',
  "JetBrains Mono": '"JetBrains Mono Variable"',
  "IBM Plex Sans": '"IBM Plex Sans Variable"',
  Figtree: '"Figtree Variable"',
};

function face(material: string, which: "display" | "text"): string {
  const stack = byMaterial("font", `font-family-${which}`, material);
  const family = /^"([^"]+)"/.exec(stack)?.[1];
  const loaded = family && FACES[family];
  if (!loaded) throw new Error(`no font package for ${stack}`);
  return `${loaded}, ${which === "display" && family === "JetBrains Mono" ? "ui-monospace, monospace" : "system-ui, -apple-system, sans-serif"}`;
}

/** Materials whose controls are drawn in ink, with hard offset shadows. */
const INK = new Set(["tactile", "hybrid"]);

/** The variables that change between light and dark. */
function colours(material: string, theme: "light" | "dark"): string[] {
  const r = roles(material, theme);
  const shadow = (name: string) => fill(byMaterial("shadow", name, material), r);
  const accent = theme === "light" ? "--accent-light" : "--accent-dark";
  // A press, drawn the material's way: ink sinks into its shadow, Signal's
  // extrusion pushes in, glass dims under the finger.
  const raised = INK.has(material)
    ? shadow("shadow-offset-sm")
    : material === "signal"
      ? shadow("shadow-neu-raised")
      : shadow("shadow-xs");
  const pressed = INK.has(material)
    ? shadow("shadow-offset-pressed")
    : material === "signal"
      ? shadow("shadow-neu-pressed")
      : shadow("shadow-inset-pressed");
  return [
    `--background: ${r.canvas}`,
    `--foreground: ${r.ink}`,
    `--surface: ${r.surface}`,
    `--surface-raised: ${r.overlay}`,
    // Below the canvas: Crystal Neuo's dark canvases are already its darkest
    // surface, so a well is the canvas darkened a little.
    `--surface-sunken: ${theme === "light" ? r["surface-3"] : `color-mix(in oklab, ${r.canvas}, black 22%)`}`,
    `--muted: ${r["surface-2"]}`,
    `--muted-foreground: ${r["ink-2"]}`,
    `--border: ${r["line-subtle"]}`,
    `--border-strong: ${r.line}`,
    // A person's own accent still wins, as it does over a palette.
    `--primary: var(${accent}, ${r.accent})`,
    `--primary-hover: ${r["accent-strong"]}`,
    `--primary-foreground: ${r["on-accent"]}`,
    `--accent: ${r["accent-soft"]}`,
    `--accent-foreground: ${r.ink}`,
    `--success: ${r.success}`,
    `--success-text: ${r["success-text"]}`,
    `--warning: ${r.warning}`,
    `--destructive: ${r["danger-text"]}`,
    `--destructive-foreground: ${r["on-state"]}`,
    `--ring: ${r.focus}`,
    `--shadow-xs: ${shadow("shadow-xs")}`,
    `--shadow-sm: ${shadow("shadow-sm")}`,
    `--shadow-md: ${shadow("shadow-md")}`,
    `--shadow-lg: ${shadow("shadow-lg")}`,
    `--cn-glass: ${r.glass}`,
    `--cn-glass-strong: ${r["glass-strong"]}`,
    `--cn-glass-rim: ${r["glass-rim"]}`,
    `--cn-glass-highlight: ${r["glass-highlight"]}`,
    `--cn-ink: ${r.line}`,
    `--cn-accent-2: ${r["accent-2"]}`,
    `--cn-raised: ${raised}`,
    `--cn-pressed: ${pressed}`,
    `--cn-overlay-shadow: ${INK.has(material) ? shadow("shadow-offset-md") : shadow("shadow-lg")}`,
  ];
}

/** The variables a material keeps in light and dark alike. */
function structure(material: string): string[] {
  const travel = byMaterial("shadow", "shadow-offset-travel", material);
  return [
    // This interface's --radius is its large corner, with controls a pixel
    // under it: one more than the material's control radius lands both.
    `--radius: ${radius("radius-md", material) + 1}px`,
    `--font-sans: ${face(material, "text")}`,
    `--cn-font-display: ${face(material, "display")}`,
    `--cn-border-width: ${byMaterial("border", "border-width-default", material)}`,
    `--cn-blur: ${byMaterial("blur", "blur-glass", material)}`,
    `--cn-blur-strong: ${byMaterial("blur", "blur-glass-strong", material)}`,
    `--cn-saturation: ${byMaterial("glass", "glass-saturation", material)}`,
    // How far a control moves when pressed: into its offset shadow for ink,
    // a pixel down for everything else.
    `--cn-press: ${INK.has(material) ? travel.replace(/(\d+)px/g, (_, n: string) => `${Number(n) - 1}px`) : "0 1px"}`,
  ];
}

const block = (selector: string, lines: string[]) =>
  `${selector} {\n${lines.map((line) => `  ${line};`).join("\n")}\n}\n`;

/*
 * Dark follows the page's own rule — an explicit .dark, or no .light — on the
 * element itself or an ancestor, so a preview of a material inside the page
 * shows it in whichever mode the page is in.
 */
function dark(material: string): string {
  const own = `[data-theme="${material}"]`;
  return [
    `.dark${own}`,
    `.dark ${own}`,
    `:root:not(.light)${own}`,
    `:root:not(.light) ${own}`,
  ].join(",\n");
}

const sections = materials.flatMap((material) => [
  // html[…] outranks the default palette's :root rules whatever order the two
  // stylesheets arrive in; the plain form is for a preview inside the page.
  block(`html[data-theme="${material}"],\n[data-theme="${material}"]`, [
    ...structure(material),
    ...colours(material, "light"),
  ]),
  block(dark(material), colours(material, "dark")),
]);

/*
 * High contrast, shared by every material and every palette: Crystal Neuo's
 * one set of roles, with every decorative shadow and all translucency gone.
 * After the materials and at least as specific as any of them, so it wins over
 * whichever is chosen.
 */
const contrast = tokens.roles["high-contrast"] as unknown as Roles;
sections.push(
  block(':root[data-contrast="more"]:is(.dark, .light, :not(.light))', [
    `--background: ${contrast.canvas}`,
    `--foreground: ${contrast.ink}`,
    `--surface: ${contrast.surface}`,
    `--surface-raised: ${contrast.overlay}`,
    `--surface-sunken: ${contrast.canvas}`,
    `--muted: ${contrast["surface-2"]}`,
    `--muted-foreground: ${contrast["ink-2"]}`,
    `--border: ${contrast.line}`,
    `--border-strong: ${contrast["line-strong"]}`,
    `--primary: ${contrast.accent}`,
    `--primary-hover: ${contrast["accent-strong"]}`,
    `--primary-foreground: ${contrast["on-accent"]}`,
    `--accent: ${contrast["accent-soft"]}`,
    `--accent-foreground: ${contrast.ink}`,
    `--success: ${contrast.success}`,
    `--success-text: ${contrast["success-text"]}`,
    `--warning: ${contrast.warning}`,
    `--destructive: ${contrast["danger-text"]}`,
    `--destructive-foreground: ${contrast["on-state"]}`,
    `--ring: ${contrast.focus}`,
    "--shadow-xs: none",
    "--shadow-sm: none",
    "--shadow-md: none",
    "--shadow-lg: none",
    `--cn-glass: ${contrast.glass}`,
    `--cn-glass-strong: ${contrast["glass-strong"]}`,
    `--cn-glass-rim: ${contrast["glass-rim"]}`,
    "--cn-glass-highlight: transparent",
    `--cn-ink: ${contrast.line}`,
    "--cn-raised: none",
    "--cn-pressed: none",
    "--cn-overlay-shadow: none",
    "--cn-blur: 0px",
    "--cn-blur-strong: 0px",
    "--cn-saturation: 100%",
  ]),
);

writeFileSync(
  output,
  `/*
 * Generated by packages/ui/scripts/build-materials.ts from Crystal Neuo
 * ${tokens.meta.version}'s tokens.json. Do not edit: change the script, or the
 * design system, and run it again.
 */

${sections.join("\n")}`,
);
// In the repository's style, so the lint that checks formatting passes as written.
spawnSync("bunx", ["prettier", "--write", output], { stdio: "inherit" });
console.log(`wrote ${output}`);
