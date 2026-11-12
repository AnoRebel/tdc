import type { ApiRequest, ApiResponse } from "@tdc/types";

/**
 * How the web client reaches the server: over HTTP, with $fetch.
 *
 * Separated from the plugin that installs it so it can be tested without a
 * Nuxt runtime. The contract both adapters are held to runs against this
 * function and against the desktop one, which is the only way to know the two
 * behave alike rather than merely compiling against the same interface.
 */

/**
 * What $fetch is, as far as this needs to know.
 *
 * Deliberately loose in its options. $fetch's own type is generic over the
 * route and the response, and narrowing this to match would tie the contract
 * to Nuxt's types — which the desktop adapter, tested by the same suite, does
 * not have.
 */
// eslint-disable-next-line @typescript-eslint/no-explicit-any
export type Fetcher = (path: string, options: any) => Promise<any>;

/**
 * Builds the transport over a fetcher.
 *
 * Passed in rather than imported so this can be exercised against a stand-in.
 * $fetch is Nuxt's, and a transport that could only be tested inside Nuxt
 * would not be held to the same contract as the desktop one, which has no
 * Nuxt at all.
 */
export function createHttpTransport(fetcher: Fetcher, apiBase: string) {
  return async function send(request: ApiRequest): Promise<ApiResponse> {
    try {
      const body = await fetcher(request.path, {
        baseURL: apiBase,
        method: request.method,
        /*
         * The contract keeps this unknown on purpose: a Wails binding takes an
         * object it marshals itself, so encoding it here would make the
         * desktop adapter parse a string this had just produced. $fetch wants
         * something narrower, and narrowing it is this adapter's job rather
         * than the contract's.
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
       * $fetch throws for both, and they are told apart by whether a response
       * came back at all: a rejection carrying a status is the server having
       * answered, and one without is nothing having answered.
       */
      const failure = cause as {
        status?: number;
        statusCode?: number;
        data?: unknown;
        response?: { headers?: Headers };
      };

      const status = failure.status ?? failure.statusCode;

      // Nothing answered. Rethrown as it is, so `readFailure` sees a cause
      // with no status — which is how the shared layer tells an unreachable
      // server from a refusal.
      if (status === undefined) throw cause;

      return {
        ok: false,
        status,
        body: failure.data,
        retryAfterSeconds: retryAfterFrom(failure.response?.headers),
      };
    }
  };
}

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
