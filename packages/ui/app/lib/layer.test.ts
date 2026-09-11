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
    /*
     * Every composable the layer defines, by the name it is called under —
     * which is not always the file's own name. Nuxt auto-imports each exported
     * function, so `useModeration.ts` exporting `useChannelBans` makes that
     * name available; a check that only read filenames would report a name the
     * layer genuinely provides as missing, which it did the moment a file with
     * several exports moved in.
     */
    const defined = new Set<string>();
    for (const name of readdirSync(join(LAYER, "app", "composables"))) {
      if (!name.endsWith(".ts") || name.endsWith(".test.ts")) continue;
      defined.add(name.replace(/\.ts$/, ""));
      const contents = readFileSync(join(LAYER, "app", "composables", name), "utf8");
      for (const match of contents.matchAll(/export\s+function\s+(use[A-Z][A-Za-z]*)/g)) {
        if (match[1]) defined.add(match[1]);
      }
    }

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

    /*
     * VueUse, auto-imported by @vueuse/nuxt — a module the layer itself
     * registers (see nuxt.config.ts), so these come with the layer rather than
     * from either application. Named explicitly rather than allowing every
     * `use*`: the point of this check is to catch a name no one provides, and a
     * blanket exception for the VueUse prefix would let those through too.
     */
    const vueuse = new Set(["useDebounceFn", "useThrottleFn", "useEventListener"]);

    const missing = new Set<string>();
    for (const { contents } of sources()) {
      for (const match of contents.matchAll(/\b(use[A-Z][A-Za-z]*)\s*\(/g)) {
        const name = match[1];
        if (!name) continue;
        // Declared in the same file, which several composables do internally.
        if (defined.has(name) || framework.has(name) || vueuse.has(name)) continue;
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
     * Components the layer registers globally from a plugin, rather than as a
     * file under components/. The date picker is one: it is a third-party
     * component the layer wraps and registers by name, and a template rendering
     * <VueDatePicker> resolves against that registration, which travels with
     * the layer to whichever application extends it.
     */
    const pluginsDir = join(LAYER, "app", "plugins");
    if (existsSync(pluginsDir)) {
      for (const entry of readdirSync(pluginsDir)) {
        if (!entry.endsWith(".ts") || entry.endsWith(".test.ts")) continue;
        const contents = readFileSync(join(pluginsDir, entry), "utf8");
        for (const match of contents.matchAll(/\.component\(\s*["']([A-Za-z0-9]+)["']/g)) {
          if (match[1]) own.add(match[1]);
        }
      }
    }

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
   * And it asks for what it needs before it waits for anything.
   *
   * A `<script setup>` with a top-level await compiles to an async setup, and
   * Vue only keeps the current component instance for the synchronous part of
   * it. A composable called after the first await therefore injects from
   * nothing — `useApi()` throws "no API client has been provided" from a file
   * that injected one correctly three lines earlier, which reads as a missing
   * plugin rather than a misplaced call.
   *
   * The application layout did exactly this: it obtained the tour after
   * awaiting the room list, and the desktop client could not open a
   * conversation at all. The web never showed it, which is the worst kind of
   * bug to leave to a second client to find.
   */
  it("calls its composables before it awaits", () => {
    const offenders: string[] = [];

    for (const { path, contents } of sources()) {
      if (!path.endsWith(".vue")) continue;

      const setup = contents.match(/<script setup[^>]*>([\s\S]*?)<\/script>/)?.[1];
      if (!setup) continue;

      /*
       * Where this file first suspends.
       *
       * Column zero only. An await inside a function body is indented and does
       * not suspend setup — matching those would flag every conversation that
       * fetches anything, which is all of them.
       */
      const firstAwait = setup.search(/^(?:await |(?:const|let) [^=]*= await )/m);
      if (firstAwait < 0) continue;

      /*
       * Likewise only unindented calls: one inside a function runs when that
       * function is called, which is not this problem. Both a declaration and a
       * bare call count — useShortcuts({...}) injects nothing today, but the
       * next composable called that way might.
       */
      for (const match of setup
        .slice(firstAwait)
        .matchAll(/^(?:(?:const|let)\s.*?=\s*)?\b(use[A-Z][A-Za-z]*)\s*\(/gm)) {
        const name = match[1];
        // useState and the router are safe; they do not inject. Keeping the
        // list short on purpose — anything else is worth moving up rather than
        // excusing here.
        if (!name || name === "useState") continue;
        offenders.push(`${path}: ${name}()`);
      }
    }

    expect(
      offenders.sort(),
      "these run after a top-level await, where there is no component instance " +
        "to inject from — move them above the first await",
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
