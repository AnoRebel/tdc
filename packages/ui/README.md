# @tdc/ui

The interface both TDC clients share.

A [Nuxt layer](https://nuxt.com/docs/getting-started/layers) rather than a
component library: an application extending it gets these composables and
components as though they were its own, and its own directories are merged over
the top. That is what lets the desktop client replace one thing and keep
everything else, instead of forking a copy that drifts from the day it is made.

## What belongs here

Anything a person sees or does that is the same in a browser and on a desktop:
the conversation, the composer, moderation, search, settings, the rules about
what a queued message means and when a tour has been seen.

## What does not

**A transport.** The one thing the two clients genuinely disagree about is how
a request reaches the server — the web client sends over `$fetch`, the desktop
client over Wails bindings, which have no URLs, headers or cookies. Nothing
here may call `fetch`; it calls `useApi()`, which returns whatever the
application provided.

**Anything needing a service worker.** There is none in a desktop webview, so
push notifications and the offline shell stay in `apps/web`.

Both rules are enforced by tests in `app/lib/layer.test.ts`, because a layer
that quietly depends on one client's runtime is a layer with one consumer and a
misleading name — and the cost lands on whoever builds the second client rather
than on whoever wrote the dependency.

## Providing a transport

An extending application must provide an `ApiClient` under `apiClientKey`
before anything calls the server:

```ts
// app/plugins/api.client.ts
import { apiClientKey } from "#layers/ui/app/composables/useApi";
import { createApiClient } from "@tdc/types";

export default defineNuxtPlugin({
  name: "tdc:api",
  enforce: "pre",
  setup(nuxtApp) {
    const client = createApiClient(async (request) => {
      // Send it however this client sends things, and describe what came back.
      return { ok: true, status: 200, body: await send(request) };
    });

    nuxtApp.vueApp.provide(apiClientKey, client);
  },
});
```

`createApiClient` in `@tdc/types` does everything that is the same regardless
of transport: which code a refusal carries, what to tell somebody, which
permission they would have needed, how long to wait. An adapter supplies only
the sending. Its contract tests run with no framework at all — no Nuxt, no
browser, no server — so both clients can be held to them.

Forgetting the provider throws on the first request, with a message saying what
is missing. That is deliberate: the alternative is an error about `undefined`
some distance from the line that caused it.

## Component names

Components in this layer are registered with a `Ui` prefix, from the directory
they sit in: `app/components/ui/data-table/DataTable.vue` becomes
`<UiDataTable>`. That is what a template should use.

The barrel `index.ts` beside each component exists for explicit imports, which
is what the vendored primitives use — but the layer's component scanner only
reads `.vue` files, so an `import { DataTable } from ".../data-table"` in a
page does not make `<DataTable>` a resolvable tag. The symptom is the element
rendering literally, with its props stringified as attributes.

## Paths inside this configuration

Anything in `nuxt.config.ts` that names a directory must resolve it absolutely:

```ts
const here = fileURLToPath(new URL(".", import.meta.url));

components: [{ path: `${here}app/components` }];
```

A relative path is resolved against whichever application is extending the
layer, not against this file. `"./app/components"` therefore points at the
_application's_ components directory, and the layer's own are registered
nowhere — with no error, since the path it found was real. The symptom is a
component that renders as nothing, which looks like a template problem rather
than a configuration one.

## Importing the layer's own files

Use `#layers/ui/app/...`. The `~` alias resolves to whichever application is
extending, which is exactly wrong for a file that lives here.

```ts
import { readFailure } from "#layers/ui/app/lib/reachability";
```

This is not a style preference. Nine components here imported `~/lib/utils`
and worked for a phase, because the web application happened to have that
file — and the desktop client, which does not, would not build. The same went
for `useAuth` and `useLastSeen`, which this layer called and that application
defined, and for the stylesheet defining the tokens these components use.

Two of the tests in `app/lib/layer.test.ts` now hold the boundary: nothing
here may import through `~` or `@`, and every composable called here must be
defined here. A dependency on the extending application is a layer with one
consumer and a misleading name.

## Everything these components need lives here

Including the parts that do not look like code. The theme
(`app/assets/css/main.css`), the Tailwind plugin that compiles it, the
`@tdc/types` alias and the pre-paint theme script are all configured in this
layer's `nuxt.config.ts` rather than in an application, so extending is enough
to get a working interface.

An application adds only what genuinely differs: the transport, and whatever
that platform alone can do.

## Themes

Three independent choices, which is why they are three values rather than one
setting listing every combination:

| Choice     | Where it lives                 | Values                       |
| ---------- | ------------------------------ | ---------------------------- |
| Palette    | `data-theme` on the root       | `default`, `congrator`       |
| Appearance | `.dark` / `.light` on the root | light, dark, or the device's |
| Accent     | `--primary` set inline         | any readable colour          |

The default palette is the absence of the attribute — the stylesheet's own
`:root` rules — so it is never written twice. Each palette defines **every**
token in **both** appearances; one that defines most of them leaves a stray
default-coloured border in an otherwise warm interface, which is the failure
nobody reports and everybody notices. `apps/web/tests/theme.spec.ts` checks
each token individually in each appearance, in a real browser, because whether
a palette arrives depends on the build and the cascade rather than on the
source.

All three are applied by the pre-paint script in the document head. Anything
applied later flashes for as long as the application takes to load.

### Reaching it

`<ThemeToggle>` in the sidebar footer carries the choices that are one press
each — the appearance and the palette — and settings carries those plus the
accent field, which needs a keyboard and somewhere to put a refusal. The
desktop client has no settings screen yet, so there the toggle is the only
way.

The active choice is announced as well as ticked: `role="menuitemradio"` with
`aria-checked`. A menu that shows the current choice only visually has told
some of its users and not the rest.

### The accent

One chosen colour becomes two shades of itself, one per appearance. Light mode
draws near-white text on the accent and dark mode near-black, so a single value
cannot be readable in both — measured, not assumed: no value clears 4.5:1
against both a near-white and a near-black foreground, and `#6366f1`, sitting
between them, manages 4.34 and 4.35.

`deriveAccent` in `app/lib/theme.ts` keeps the hue and chroma and moves only
the lightness, from each end, until each appearance has a shade its own text
reads on. What appears is recognisably the colour that was asked for. The pair
is applied as `--accent-light` and `--accent-dark`, which each palette's own
blocks read as the default for `--primary`.

Because a readable shade can be found for any colour that can be parsed, the
only refusal is for a value that is not a colour. The server applies the same
rule — it has to accept exactly what the client can derive, or a setting saves
and then does nothing.

The conversion is oklch → sRGB → relative luminance, and the tests pin it to
values the specification fixes rather than to its own output.

### Where the choice lives

On the account, not in a browser: `/api/me/preferences/appearance` holds the
palette, the appearance and the accent, so somebody who chooses the warm
palette on the desktop client sees it on the web one. An operator sets the
instance's defaults through `brand.palette` and `brand.accent`.

`useAppearanceSync` resolves the two in one place — this person's choice, then
the operator's, then the palette's own — and each of the three resolves
independently. Applying them separately is what the web application used to do,
with a watcher that cleared the accent whenever the operator had set none,
silently wiping a person's own choice every time server information reloaded.

## Introductions

A tour is worth giving to somebody who has not seen it and an obstacle to
everybody else — including the same person on a second device, which is why the
record lives on the account at `/api/me/preferences/tours` rather than in one
browser. The browser keeps a copy that answers immediately, so a tour dismissed
a moment ago does not reappear while a request is in flight; the account's copy
wins once it arrives.

The server stores what the client defines and hands it back unchanged. It does
not know what a tour is, which is what lets a later phase add one without a
migration or a server that refuses a client newer than itself.

Skipping counts as seeing. Somebody who dismissed a tour said what they thought
of it more clearly than somebody who sat through it — but an interruption is
not a dismissal, so a tour left mid-step stays offered and resumes where it
stopped. Settings can forget one, and that reaches the account too: asking to
be shown around again should not work on one device only.

## Tests

Run from `apps/web`, which is where the test runner is configured:

```sh
bun run --cwd apps/web test
```

## Languages

English and Swahili, configured in this layer so both clients read the same
files. Translations live in `locales/`, one JSON file per language.

```
packages/ui/locales/en.json
packages/ui/locales/sw.json
```

English is the fallback. A key missing from another language renders in
English rather than as the key itself — somebody meeting `room.waitingToSend`
has met a bug wearing the interface's clothes, and English is not ideal for a
Swahili speaker but is a great deal better than punctuation.

No locale prefix in the URL. The desktop client has no URLs to put one in, and
a shared layer whose routing depends on a browser is a layer with one
consumer; the choice lives in a cookie, which both clients have.

### Adding a string

Put it in `en.json`, translate it in `sw.json`, and call `$t("its.key")` from
the template. `locales/locales.test.ts` will fail if the two files disagree —
if a key exists in one and not the other, if a placeholder like `{room}` is
dropped in translation, or if anything is left empty.

### What is not translated yet

The mechanism is complete; wiring every screen through it is not. To see what
remains:

```sh
bun run --cwd apps/web translations
```

That reports literal strings still sitting in templates, where no language but
English can reach them. It reports rather than fails, because blocking work on
a half-finished screen helps nobody — the number going down is the point.

The Swahili was written by somebody who is not a native speaker. Corrections
are welcome and are the kind of contribution that needs no discussion first.
