/// <reference lib="webworker" />

import {
  cleanupOutdatedCaches,
  createHandlerBoundToURL,
  precacheAndRoute,
} from "workbox-precaching";
import { NavigationRoute, registerRoute } from "workbox-routing";

/*
 * The service worker.
 *
 * Written out rather than generated, because a generated one cannot be given a
 * push handler — and receiving a notification while the application is closed
 * is the whole reason a phone installs this. Everything the generated worker
 * did is still here; what is added is the part below the caching.
 *
 * This file runs in a worker with no DOM and no application: it cannot read a
 * store, call a composable, or ask the user anything. What it knows is what
 * the push payload tells it.
 */

declare const self: ServiceWorkerGlobalScope & {
  __WB_MANIFEST: Array<{ url: string; revision: string | null }>;
};

/*
 * The built assets, injected here at build time.
 *
 * Their names carry content hashes, so a file that is present is the right
 * one and stale entries can be dropped outright.
 */
precacheAndRoute(self.__WB_MANIFEST);
cleanupOutdatedCaches();

/*
 * What must never be answered from the cache.
 *
 * A single-page application answers any navigation with its shell, which is
 * what makes a deep link work offline. Unqualified, that swallows the API and
 * the live connection with it: a socket upgrade answered with a cached
 * document does not fail in a way anything reports — it simply never connects,
 * and the application looks merely quiet.
 *
 * TDC serves its live connection at /api/live rather than a path of its own,
 * so one rule covers both.
 */
const denylist = [
  /^\/api\//,
  // Anything with an extension, which a browser fetches rather than navigates
  // to. A download answered with the shell is a corrupt file rather than an
  // error, and a corrupt file is harder to diagnose.
  /\.[a-z0-9]+$/i,
];

registerRoute(new NavigationRoute(createHandlerBoundToURL("/"), { denylist }));

/*
 * A new version, applied when the application asks rather than at once.
 *
 * Reloading under somebody who is typing loses what they were writing. The
 * application prompts, and only then does the waiting worker take over.
 */
self.addEventListener("message", (event) => {
  if (event.data?.type === "SKIP_WAITING") void self.skipWaiting();
});

/** What the server sends. */
interface PushPayload {
  title?: string;
  body?: string;
  room?: string;
  event?: string;
}

/*
 * A notification arriving while the application is closed.
 *
 * The payload has already been decided by the server, including whether it may
 * carry message content — somebody who asked for content to be hidden has that
 * honoured before it is encrypted, not here. This worker displays what it is
 * given and adds nothing.
 */
/*
 * What a push carried, or nothing.
 *
 * A payload that is absent, empty, or not the JSON this server sends all reach
 * the same place: a notification saying something happened. Anything else
 * would mean a thrown error inside a push handler, and a handler that throws
 * shows no notification at all.
 */
function readPayload(event: PushEvent): PushPayload {
  try {
    return (event.data?.json() as PushPayload | undefined) ?? {};
  } catch {
    return {};
  }
}

self.addEventListener("push", (event) => {
  /*
   * A push with no readable payload still shows something.
   *
   * Browsers require that a push permission is not used silently, and some
   * services deliver an empty push to keep a subscription alive. Showing
   * nothing risks the browser revoking the permission; showing a bare notice
   * is honest about what is known.
   */
  const payload = readPayload(event);

  const title = payload.title || "TDC";
  const body = payload.body || "Something happened";

  event.waitUntil(
    self.registration.showNotification(title, {
      body,
      icon: "/icon-192.png",
      badge: "/icon-192.png",
      /*
       * Grouped by conversation, so ten messages in one channel replace each
       * other rather than filling a lock screen. Without a tag, a busy
       * conversation buries everything else a phone was showing.
       */
      tag: payload.room ? `room:${payload.room}` : "tdc",
      // Where activating it should go, since a notification is only useful if
      // it takes somebody to the thing it is about.
      data: { room: payload.room, event: payload.event },
    }),
  );
});

/*
 * Activating a notification goes to the conversation it is about.
 *
 * An existing window is focused rather than a second one opened: somebody with
 * the application already open on another channel wants that window taken
 * somewhere, not a duplicate.
 */
self.addEventListener("notificationclick", (event) => {
  event.notification.close();

  const data = event.notification.data as { room?: string } | undefined;
  const target = data?.room ? `/rooms/${data.room}` : "/";

  event.waitUntil(
    (async () => {
      const clients = await self.clients.matchAll({
        type: "window",
        includeUncontrolled: true,
      });

      for (const client of clients) {
        if ("focus" in client) {
          // Navigate the window that is already open, then bring it forward.
          if ("navigate" in client) await client.navigate(target);
          await client.focus();
          return;
        }
      }

      await self.clients.openWindow(target);
    })(),
  );
});
