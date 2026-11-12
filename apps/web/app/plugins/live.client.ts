import { liveSocketKey } from "#layers/ui/app/composables/useLiveSocket";

/**
 * Installs the web client's live socket.
 *
 * A WebSocket, opened against the API's origin with the session cookie the
 * browser carries. The URL is the configured API base with its scheme moved
 * from http to ws — in production that is the page's own origin, and in
 * development the API on its own port. The desktop client provides a different
 * socket, because there the session is not in the webview to carry.
 */
export default defineNuxtPlugin({
  name: "tdc:live",
  enforce: "pre",
  setup(nuxtApp) {
    const apiBase = useRuntimeConfig().public.apiBase;

    nuxtApp.vueApp.provide(liveSocketKey, (handlers) => {
      const base = apiBase || window.location.origin;
      const socket = new WebSocket(base.replace(/^http/, "ws") + "/api/live");

      socket.onopen = () => handlers.onOpen();
      socket.onmessage = (message) => handlers.onMessage(message.data as string);
      socket.onclose = () => handlers.onClose();
      socket.onerror = () => socket.close(); // onclose follows and reconnects.

      return {
        send(message) {
          if (socket.readyState === WebSocket.OPEN) {
            socket.send(JSON.stringify(message));
          }
        },
        close() {
          socket.close();
        },
      };
    });
  },
});
