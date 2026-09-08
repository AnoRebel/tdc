import { createApiClient, type ApiClient, type ApiRequest, type ApiResponse } from "@tdc/types";

/**
 * Calls the TDC server over HTTP.
 *
 * Only the sending lives here. What a refusal means, which code it carries and
 * how it reaches a caller are decided once in the shared client, so the
 * desktop client — which speaks to the same server over Wails bindings with no
 * statuses, headers or cookies — answers to the same contract rather than a
 * parallel one that drifts.
 */
export function useApi(): ApiClient {
  const apiBase = useRuntimeConfig().public.apiBase;

  return createApiClient(async (request: ApiRequest): Promise<ApiResponse> => {
    const response = await fetch(`${apiBase}${request.path}`, {
      method: request.method,
      headers: { "Content-Type": "application/json" },
      body: request.body === undefined ? undefined : JSON.stringify(request.body),
      // Sends and accepts the session cookie. The browser stores it as
      // HttpOnly, so no script here ever sees the token.
      credentials: "include",
    });

    /*
     * A response that carried nothing, which is not the same as one that
     * carried null. Several endpoints answer 204 to say the request succeeded
     * and there is nothing to report.
     */
    if (response.status === 204) {
      return { ok: response.ok, status: response.status };
    }

    const text = await response.text();

    return {
      ok: response.ok,
      status: response.status,
      /*
       * Parsed here rather than in the shared client, because what counts as
       * a body is a transport's business: this one has text that may or may
       * not be JSON, and a Wails binding has an object already.
       */
      body: text ? safelyParse(text) : undefined,
      retryAfterSeconds: retryAfterFrom(response),
    };
  });
}

/**
 * Reads a body that should be JSON.
 *
 * A server behind a proxy can answer with the proxy's own HTML — a gateway
 * error page, most often — and throwing while trying to explain a failure
 * replaces a useful message with a parse error about the wrong thing.
 */
function safelyParse(text: string): unknown {
  try {
    return JSON.parse(text);
  } catch {
    return { message: text.slice(0, 200) };
  }
}

function retryAfterFrom(response: Response): number | undefined {
  const header = response.headers.get("Retry-After");
  if (!header) return undefined;

  const seconds = Number.parseInt(header, 10);
  return Number.isFinite(seconds) ? seconds : undefined;
}
