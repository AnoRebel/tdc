import { Events } from "@wailsio/runtime";

import { liveSocketKey } from "#layers/ui/app/composables/useLiveSocket";
import * as Api from "#bindings/api";

/**
 * Installs the desktop client's live socket, over the Go bridge.
 *
 * The webview cannot open an authenticated socket — the session is in the Go
 * client's jar, not here — so Go opens it and forwards each frame as an event.
 * This turns that into the small socket the shared connection expects: Connect
 * opens it, the three events become the open/message/close callbacks, and a
 * message the connection sends is handed back to Go to write.
 *
 * The event names match the Go side's constants; a mismatch would be a socket
 * that opens and is never heard from.
 */

// Kept in step with internal/api/live.go.
const frameEvent = "tdc:live:frame";
const openEvent = "tdc:live:open";
const closeEvent = "tdc:live:close";

export default defineNuxtPlugin({
  name: "tdc:live",
  enforce: "pre",
  setup(nuxtApp) {
    nuxtApp.vueApp.provide(liveSocketKey, (handlers) => {
      // Go emits a frame's text as the event's first datum. Wails delivers it
      // wrapped, so the payload is read off the event.
      const offFrame = Events.On(frameEvent, (event: { data?: unknown }) => {
        const data = Array.isArray(event.data) ? event.data[0] : event.data;
        if (typeof data === "string") handlers.onMessage(data);
      });
      const offOpen = Events.On(openEvent, () => handlers.onOpen());
      const offClose = Events.On(closeEvent, () => handlers.onClose());

      // Opening is asynchronous; a failure to dial arrives as a close, which
      // the shared machinery treats as a dropped connection and retries — the
      // same path a socket that opened and then dropped takes.
      void Api.LiveBridge.Connect().catch(() => handlers.onClose());

      return {
        send(message) {
          void Api.LiveBridge.Send(message).catch(() => {
            // Sent before the socket was open, or after it closed. The shared
            // machinery replays every subscription on the next open, so a lost
            // message here costs nothing.
          });
        },
        close() {
          offFrame();
          offOpen();
          offClose();
          void Api.LiveBridge.Close();
        },
      };
    });
  },
});
