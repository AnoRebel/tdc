import type { ApiClient, ApiRequest, ApiResponse, ApiTransport } from "./client";
import { ApiError, isErrorCode, type ApiErrorBody, type ErrorCode } from "./errors";

/**
 * Builds an {@link ApiClient} on any transport.
 *
 * Everything that is the same regardless of how bytes travel lives here:
 * deciding the method, carrying the body, and turning a refusal into an
 * {@link ApiError} with a code shared code can branch on. An adapter supplies
 * only the sending.
 *
 * Written as a plain function rather than a composable so it can be tested
 * without a framework — the desktop client has no Nuxt runtime at all, and a
 * contract that can only be exercised inside one is a contract neither client
 * can be held to.
 */
export function createApiClient(send: ApiTransport): ApiClient {
  async function request<T>(
    method: ApiRequest["method"],
    path: string,
    body?: unknown,
  ): Promise<T> {
    const response = await send({ method, path, body });

    if (!response.ok) throw errorFrom(response);

    // A response that carried nothing, which is not the same as one that
    // carried null: a 204 means the request succeeded and had nothing to say.
    return response.body as T;
  }

  return {
    get: <T>(path: string) => request<T>("GET", path),
    post: <T>(path: string, body?: unknown) => request<T>("POST", path, body),
    put: <T>(path: string, body?: unknown) => request<T>("PUT", path, body),
    patch: <T>(path: string, body?: unknown) => request<T>("PATCH", path, body),
    delete: <T>(path: string) => request<T>("DELETE", path),
  };
}

/**
 * Turns a refusal into the shared error shape.
 *
 * The message is the server's own words where it gave any, because a server
 * knows why it refused and this does not. What is derived here is the code,
 * which is the thing shared code decides on.
 */
function errorFrom(response: ApiResponse): ApiError {
  const body = (response.body ?? {}) as Record<string, unknown>;

  const message =
    typeof body.message === "string" && body.message
      ? body.message
      : "The server could not complete the request.";

  const shape: ApiErrorBody = {
    code: codeFor(response),
    message,
    requiredPermission: permissionNamedIn(message),
    retryAfterSeconds: response.retryAfterSeconds,
  };

  return new ApiError(shape, response.status ?? 0);
}

/**
 * The code a refusal carries.
 *
 * From the status where there is one. A transport without statuses — Wails
 * bindings, for instance — is expected to have put a code in the body itself,
 * since it knows what went wrong and this cannot infer it from nothing.
 */
function codeFor(response: ApiResponse): ErrorCode {
  const body = (response.body ?? {}) as Record<string, unknown>;

  if (typeof body.code === "string" && isErrorCode(body.code)) return body.code;

  switch (response.status) {
    case 400:
      return "invalid";
    case 401:
      return "unauthenticated";
    case 403:
      return "forbidden";
    case 404:
      return "not-found";
    case 409:
      return "conflict";
    case 429:
      return "rate-limited";
    case 503:
      return "unavailable";
    default:
      /*
       * A refusal with no status at all is treated as the server failing
       * rather than refusing. That is the safer of the two: something
       * internal invites a retry, where "invalid" tells somebody their
       * request was wrong when nothing here knows that.
       */
      if (response.status === undefined) return "internal";
      return response.status >= 500 ? "internal" : "invalid";
  }
}

/**
 * The permission a refusal named, if it named one.
 *
 * So the interface can say what somebody would need rather than only that they
 * were refused, which is the difference between a dead end and a next step.
 */
function permissionNamedIn(message: string): string | undefined {
  return message.match(/requires permission "([^"]+)"/)?.[1];
}
