#!/usr/bin/env bun
/**
 * Propagates the version in the root VERSION file to every place that needs to
 * declare one.
 *
 * The VERSION file is the single source of truth. Nothing else may introduce an
 * independent version number: every released artifact — server binary,
 * container image, web bundle and desktop installer — has to report the same
 * string, and the only reliable way to achieve that is to derive them all from
 * one place.
 *
 * Run with --check to verify without writing. This is what continuous
 * integration uses, so a hand-edited version in one file fails the build rather
 * than shipping a set of artifacts that disagree about what they are.
 */

import { readFileSync, writeFileSync, existsSync } from "node:fs";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const checkOnly = process.argv.includes("--check");

const version = readFileSync(join(root, "VERSION"), "utf8").trim();

if (!/^\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$/.test(version)) {
  console.error(`VERSION does not contain a valid semantic version: "${version}"`);
  process.exit(1);
}

/** A file to keep in step, and how to read and rewrite its version. */
interface Target {
  /** Path relative to the repository root. */
  path: string;
  /** Returns the version currently declared, or null when the file is absent. */
  read: (text: string) => string | null;
  /** Returns the text with the version replaced. */
  write: (text: string, next: string) => string;
  /** What the file is given, where that is not the version itself. */
  value?: string;
}

const jsonVersion: Target["read"] = (text) => JSON.parse(text).version ?? null;

/**
 * Rewrites a JSON "version" field textually rather than by reparsing, so that
 * key order, indentation and trailing newline survive untouched. Reserialising
 * would reformat files that people also edit by hand.
 */
const writeJsonVersion: Target["write"] = (text, next) =>
  text.replace(/("version"\s*:\s*")[^"]*(")/, `$1${next}$2`);

/*
 * The platform files Wails builds the desktop installers from. Windows and
 * macOS take only numbers there — no "-beta.1" — and MSIX wants four parts, so
 * they carry the version's numeric core. Each is edited in place: `wails3
 * update build-assets` would regenerate them from config.yml, and with them
 * everything else we changed in those files, such as the macOS usage
 * descriptions calls depend on.
 */
const core = version.replace(/-.*$/, "");
const fourPart = `${core}.0`;

/** A target whose every match of a pattern must hold the same value. */
function everyMatch(path: string, pattern: RegExp, value: string): Target {
  return {
    path,
    value,
    read: (text) => {
      const found = [...text.matchAll(pattern)].map((match) => match[2]);
      if (found.length === 0) return null;
      // Any disagreement among them reads as drift, so the write fixes all.
      return found.every((each) => each === value) ? version : (found[0] ?? null);
    },
    write: (text) => text.replace(pattern, (_, before, _old, after) => `${before}${value}${after}`),
  };
}

const targets: Target[] = [
  { path: "package.json", read: jsonVersion, write: writeJsonVersion },
  { path: "apps/docs/package.json", read: jsonVersion, write: writeJsonVersion },
  everyMatch(
    "apps/desktop/build/darwin/Info.plist",
    /(<key>CFBundle(?:Short)?VersionString<\/key>\s*<string>|<key>CFBundleVersion<\/key>\s*<string>)([^<]*)(<\/string>)/g,
    core,
  ),
  everyMatch(
    "apps/desktop/build/darwin/Info.dev.plist",
    /(<key>CFBundle(?:Short)?VersionString<\/key>\s*<string>|<key>CFBundleVersion<\/key>\s*<string>)([^<]*)(<\/string>)/g,
    core,
  ),
  everyMatch(
    "apps/desktop/build/windows/info.json",
    /("(?:file_version|ProductVersion)"\s*:\s*")([^"]*)(")/g,
    core,
  ),
  everyMatch(
    "apps/desktop/build/windows/wails.exe.manifest",
    /(<assemblyIdentity type="win32" name="net\.anorebel\.tdc" version=")([^"]*)(")/g,
    core,
  ),
  everyMatch(
    "apps/desktop/build/windows/nsis/wails_tools.nsh",
    /(!define INFO_PRODUCTVERSION ")([^"]*)(")/g,
    core,
  ),
  everyMatch(
    "apps/desktop/build/windows/msix/app_manifest.xml",
    /(\n\s*Version=")([^"]*)(")/g,
    fourPart,
  ),
  everyMatch(
    "apps/desktop/build/windows/msix/template.xml",
    /(\n\s*Version=")([^"]*)(")/g,
    fourPart,
  ),
  everyMatch("apps/desktop/build/linux/nfpm/nfpm.yaml", /(^version:\s*")([^"]*)(")/gm, version),
  { path: "packages/types/package.json", read: jsonVersion, write: writeJsonVersion },
  { path: "packages/ui/package.json", read: jsonVersion, write: writeJsonVersion },
  { path: "apps/web/package.json", read: jsonVersion, write: writeJsonVersion },
  { path: "apps/desktop/frontend/package.json", read: jsonVersion, write: writeJsonVersion },
  {
    /*
     * Wails v3 reads the desktop application's version from this file.
     *
     * Matched under `info:` specifically. The file opens with `version: '3'`,
     * which is the Taskfile schema version — an unanchored match takes that
     * one, reports it as drift, and then overwrites it, breaking the build
     * configuration to fix a version that was never wrong.
     */
    path: "apps/desktop/build/config.yml",
    read: (text) =>
      text.match(/^info:\n(?:[ \t]+.*\n)*?[ \t]+version:\s*"?([^"\n]+)"?/m)?.[1]?.trim() ?? null,
    write: (text, next) =>
      text.replace(/^(info:\n(?:[ \t]+.*\n)*?[ \t]+version:\s*)"?[^"\n]+"?/m, `$1"${next}"`),
  },
  {
    // Compiled into the server so it can report its own version at runtime.
    path: "server/internal/buildinfo/version.go",
    read: (text) => text.match(/Version\s*=\s*"([^"]*)"/)?.[1] ?? null,
    write: (text, next) => text.replace(/(Version\s*=\s*")[^"]*(")/, `$1${next}$2`),
  },
  {
    // And into the desktop client, which shows it on its About screen and
    // compares it against what a release feed offers.
    path: "apps/desktop/internal/buildinfo/version.go",
    read: (text) => text.match(/Version\s*=\s*"([^"]*)"/)?.[1] ?? null,
    write: (text, next) => text.replace(/(Version\s*=\s*")[^"]*(")/, `$1${next}$2`),
  },
];

let drifted = 0;
let updated = 0;

for (const target of targets) {
  const absolute = join(root, target.path);
  if (!existsSync(absolute)) continue;

  const text = readFileSync(absolute, "utf8");
  const current = target.read(text);
  if (current === version) continue;

  if (checkOnly) {
    console.error(`${target.path}: has "${current}", expected "${target.value ?? version}"`);
    drifted++;
    continue;
  }

  writeFileSync(absolute, target.write(text, version));
  console.log(`${target.path}: ${current} -> ${target.value ?? version}`);
  updated++;
}

if (checkOnly) {
  if (drifted > 0) {
    console.error(
      `\n${drifted} file(s) disagree with VERSION (${version}). Run "bun run version:sync".`,
    );
    process.exit(1);
  }
  console.log(`All versions match VERSION (${version}).`);
} else {
  console.log(
    updated === 0 ? `Already at ${version}.` : `Updated ${updated} file(s) to ${version}.`,
  );
}
