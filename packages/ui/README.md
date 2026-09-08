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

## Importing the layer's own files

Use `#layers/ui/app/...`. The `~` alias resolves to whichever application is
extending, which is exactly wrong for a file that lives here.

```ts
import { readFailure } from "#layers/ui/app/lib/reachability";
```

## Tests

Run from `apps/web`, which is where the test runner is configured:

```sh
bun run --cwd apps/web test
```
