import { apiClientKey } from "#layers/ui/app/composables/useApi";
import { createApiClient, type ApiRequest, type ApiResponse } from "@tdc/types";

/**
 * How this client reaches the server: over HTTP, with $fetch.
 *
 * The only thing the web and desktop clients disagree about. Everything above
 * this — every composable in the shared layer, and every screen built on them
 * — is written against the interface and does not know which is underneath.
 *
 * $fetch rather than the global one: it resolves the base URL, parses the body
 * and gives a failure a shape, so what is left here is mapping that shape onto
 * the transport contract rather than reimplementing any of it.
 */
export default defineNuxtPlugin({
  name: "tdc:api",
  // Before anything that might call the server on mount.
  enforce: "pre",
  setup(nuxtApp) {
    const apiBase = useRuntimeConfig().public.apiBase;

    const client = createApiClient(async (request: ApiRequest): Promise<ApiResponse> => {
      try {
        const body = await $fetch(request.path, {
          baseURL: apiBase,
          method: request.method,
          /*
           * The contract keeps this unknown on purpose: a Wails binding takes
           * an object it marshals itself, so encoding it here would make the
           * desktop adapter parse a string this had just produced. $fetch
           * wants something narrower, and narrowing it is this adapter's job
           * rather than the contract's.
           */
          body: request.body as Record<string, unknown> | undefined,
          // Sends and accepts the session cookie. The browser holds it as
          // HttpOnly, so no script here ever sees the token.
          credentials: "include",
        });

        return { ok: true, status: 200, body };
      } catch (cause) {
        /*
         * A refusal, or a network that was not there.
         *
         * $fetch throws for both, and they are told apart by whether a
         * response came back at all: a rejection carrying a status is the
         * server having answered, and one without is nothing having answered.
         */
        const failure = cause as {
          status?: number;
          statusCode?: number;
          data?: unknown;
          response?: { headers?: Headers };
        };

        const status = failure.status ?? failure.statusCode;

        if (status === undefined) throw cause;

        return {
          ok: false,
          status,
          body: failure.data,
          retryAfterSeconds: retryAfterFrom(failure.response?.headers),
        };
      }
    });

    nuxtApp.vueApp.provide(apiClientKey, client);
  },
});

/**
 * How long the server asked us to wait, if it said.
 *
 * A header, which is an HTTP idea — the shared contract carries the number
 * instead, so nothing above here has to know where it came from.
 */
function retryAfterFrom(headers: Headers | undefined): number | undefined {
  const header = headers?.get("Retry-After");
  if (!header) return undefined;

  const seconds = Number.parseInt(header, 10);
  return Number.isFinite(seconds) ? seconds : undefined;
}
