import type { ApiClient } from "@tdc/types";

/**
 * The key an application provides its transport under.
 *
 * A symbol rather than a string, so nothing can collide with it by accident
 * and so a missing provider is a clear failure rather than a silent undefined.
 */
export const apiClientKey = Symbol("tdc.api") as InjectionKey<ApiClient>;

/**
 * The server, as everything above this layer sees it.
 *
 * How bytes actually travel is the one thing the two clients disagree about,
 * so it is injected rather than chosen here: the web client provides an
 * adapter over $fetch, and the desktop client one over Wails bindings. Every
 * composable in this layer is written against the interface and works
 * unchanged in both.
 */
export function useApi(): ApiClient {
  const client = inject(apiClientKey, null);

  if (!client) {
    /*
     * Loud, and early. An application that forgot to provide a transport would
     * otherwise fail on its first request with an error about undefined, some
     * distance from the line that caused it.
     */
    throw new Error(
      "No API client has been provided. An application extending @tdc/ui must " +
        "provide one under apiClientKey — see the web client's api.client.ts plugin.",
    );
  }

  return client;
}
