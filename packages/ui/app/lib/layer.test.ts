import { existsSync, readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";

import { describe, expect, it } from "vitest";

/*
 * What makes this shareable, checked rather than assumed.
 *
 * A layer that quietly depends on one application's runtime is a layer with
 * one consumer and a misleading name. These are the properties that let a
 * second client — the desktop one, which has no fetch and no service worker —
 * extend it rather than fork it.
 */

const LAYER = join(import.meta.dirname, "..", "..");

/** Every source file in the layer, so a new one cannot slip past these. */
function sources(): { path: string; contents: string }[] {
  const found: { path: string; contents: string }[] = [];

  const walk = (directory: string) => {
    if (!existsSync(directory)) return;

    for (const entry of readdirSync(directory, { withFileTypes: true })) {
      const full = join(directory, entry.name);
      if (entry.isDirectory()) walk(full);
      else if (/\.(ts|vue)$/.test(entry.name) && !entry.name.endsWith(".test.ts")) {
        found.push({ path: full.slice(LAYER.length + 1), contents: readFileSync(full, "utf8") });
      }
    }
  };

  walk(join(LAYER, "app"));
  return found;
}

/** Every name a file imports explicitly, however the import is formatted. */
function importedNames(contents: string): Set<string> {
  const names = new Set<string>();

  for (const match of contents.matchAll(/import\s+(?:type\s+)?\{([^}]*)\}\s+from/g)) {
    const clause = match[1] ?? "";
    for (const part of clause.split(",")) {
      // "Foo as Bar" binds Bar; "type Foo" binds Foo.
      const bound = part
        .trim()
        .replace(/^type\s+/, "")
        .split(/\s+as\s+/)
        .pop();
      if (bound) names.add(bound.trim());
    }
  }

  return names;
}

describe("what the shared layer may depend on", () => {
  /*
   * The one thing that genuinely differs between the two clients.
   *
   * The web client sends over fetch and the desktop one over Wails bindings,
   * which have no URLs, headers or cookies. A file here that calls fetch has
   * made itself web-only without saying so, and the cost lands on whoever
   * tries to build the desktop client rather than on whoever wrote it.
   */
  it("never reaches for a transport itself", () => {
    const offenders = sources()
      .filter(({ contents }) => /\bfetch\s*\(|new XMLHttpRequest|\$fetch\s*\(/.test(contents))
      .map(({ path }) => path);

    expect(offenders, "these would only work in a browser").toEqual([]);
  });

  /*
   * Service workers do not exist in a Wails webview.
   *
   * Anything depending on one belongs in the web application, which is where
   * push notifications and the offline shell already live.
   */
  it("never depends on a service worker", () => {
    const offenders = sources()
      .filter(({ contents }) => /serviceWorker|PushManager/.test(contents))
      .map(({ path }) => path);

    expect(offenders, "these cannot work in a desktop webview").toEqual([]);
  });

  /*
   * Nothing here may reach into whichever application is extending.
   *
   * `~` and `@` resolve to the extending application's directory, not to this
   * one — so a component here importing "~/lib/utils" gets the web
   * application's copy, and the desktop client, which has no such file, does
   * not build. It went unnoticed for a whole phase because there was only one
   * consumer, and one consumer is exactly the condition this layer exists to
   * end.
   *
   * The layer's own files are reached by the #layers/ui alias, which resolves
   * here regardless of who is extending.
   */
  it("never imports through the extending application's alias", () => {
    const offenders = sources()
      .filter(({ contents }) => /from\s+["'][~@]\//.test(contents))
      .map(({ path }) => path);

    expect(
      offenders,
      "these resolve into whichever application extends this layer, " +
        "so they work for one client and break the other — use #layers/ui/app/...",
    ).toEqual([]);
  });

  /*
   * And the composables it calls are ones it defines.
   *
   * Nuxt auto-imports by name, so a component here calling useAuth() compiles
   * against whatever the extending application happens to define — or nothing
   * at all. The failure is a type error in the other client, a long way from
   * the file that caused it.
   */
  it("defines every composable its own files call", () => {
    const defined = new Set(
      readdirSync(join(LAYER, "app", "composables"))
        .filter((name) => name.endsWith(".ts") && !name.endsWith(".test.ts"))
        .map((name) => name.replace(/\.ts$/, "")),
    );

    /*
     * Nuxt's own auto-imports, which every application has: they come from the
     * framework rather than from either client, so calling one is not a
     * dependency on anybody.
     */
    const framework = new Set([
      "useHead",
      "useRoute",
      "useRouter",
      "useState",
      "useFetch",
      "useAsyncData",
      "useLazyAsyncData",
      "useRuntimeConfig",
      "useNuxtApp",
      "useCookie",
      "useError",
      "useSeoMeta",
      "useRequestHeaders",
      "useAppConfig",
      "useId",
      "useTemplateRef",
      "useSlots",
      "useAttrs",
      "useModel",
      "useLocalePath",
      "useSwitchLocalePath",
      "useI18n",
      "useNuxtData",
      "useRequestURL",
      "useRequestEvent",
      "useLoadingIndicator",
    ]);

    const missing = new Set<string>();
    for (const { contents } of sources()) {
      for (const match of contents.matchAll(/\b(use[A-Z][A-Za-z]*)\s*\(/g)) {
        const name = match[1];
        if (!name) continue;
        // Declared in the same file, which several composables do internally.
        if (defined.has(name) || framework.has(name)) continue;
        if (contents.includes(`function ${name}`) || contents.includes(`const ${name} =`)) continue;
        // Imported from a package rather than auto-imported. Read from the
        // import statements themselves: a name can sit on its own line in a
        // multi-line import, which a substring check on "import {" misses.
        if (importedNames(contents).has(name)) continue;
        missing.add(name);
      }
    }

    expect(
      [...missing].sort(),
      "these are called here and defined by an application rather than by this layer",
    ).toEqual([]);
  });

  /*
   * And the components it renders are ones it ships.
   *
   * Nuxt auto-registers components by name across the layer and the
   * application together, so a component here rendering <NotificationPanel>
   * resolves against whichever application is extending — or against nothing.
   * The sidebar did exactly this for a phase: it worked in the web client and
   * would have left a hole in the desktop one's footer.
   */
  it("ships every component its own files render", () => {
    const own = new Set<string>();
    const walkComponents = (directory: string) => {
      if (!existsSync(directory)) return;
      for (const entry of readdirSync(directory, { withFileTypes: true })) {
        if (entry.isDirectory()) walkComponents(join(directory, entry.name));
        else if (entry.name.endsWith(".vue")) own.add(entry.name.replace(/\.vue$/, ""));
      }
    };
    walkComponents(join(LAYER, "app", "components"));

    /*
     * What Nuxt itself provides, plus the vendored primitives imported by
     * name. Rendering one of these is not a dependency on an application.
     */
    const framework = new Set([
      "NuxtLink",
      "NuxtPage",
      "NuxtLayout",
      "ClientOnly",
      "Icon",
      "Teleport",
      "Transition",
      "TransitionGroup",
      "KeepAlive",
      "Suspense",
      "Component",
      "NuxtImg",
      "NuxtPicture",
      "NuxtLoadingIndicator",
      "NuxtErrorBoundary",
    ]);

    const missing = new Set<string>();
    for (const { path, contents } of sources()) {
      /*
       * Templates only. `Array<Room>` and `Ref<User>` are generics, not tags,
       * and a scan of whole files reports every type this layer names.
       */
      if (!path.endsWith(".vue")) continue;
      const template = contents.match(/<template>([\s\S]*)<\/template>/)?.[1];
      if (!template) continue;

      const imported = importedNames(contents);
      for (const match of template.matchAll(/<([A-Z][A-Za-z0-9]*)[\s/>]/g)) {
        const name = match[1];
        if (!name) continue;
        // Ui-prefixed names are this layer's own, resolved by pathPrefix.
        if (own.has(name) || own.has(name.replace(/^Ui/, ""))) continue;
        if (framework.has(name) || imported.has(name)) continue;
        missing.add(name);
      }
    }

    expect(
      [...missing].sort(),
      "these are rendered here and defined by an application rather than by this layer",
    ).toEqual([]);
  });

  /*
   * The layer names itself, which is what makes #layers/ui resolve.
   *
   * Without it the alias is derived from a path, and every import in both
   * clients breaks the day this directory moves.
   */
  it("names itself so its own files can be imported", () => {
    const config = readFileSync(join(LAYER, "nuxt.config.ts"), "utf8");

    expect(config).toContain('name: "ui"');
  });
});
