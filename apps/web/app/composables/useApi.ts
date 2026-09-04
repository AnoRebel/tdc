import { ApiError, type ApiErrorBody, type ErrorCode } from "@tdc/types";

/**
 * Calls the TDC server.
 *
 * Every failure is normalised into ApiError, so callers branch on a code
 * rather than on a status or on prose. The desktop client will do the same
 * over Wails bindings, which have no status codes at all — which is why the
 * code, not the status, is what shared code is written against.
 */
export function useApi() {
  const apiBase = useRuntimeConfig().public.apiBase;

  async function request<T>(path: string, options: RequestInit = {}): Promise<T> {
    const response = await fetch(`${apiBase}${path}`, {
      ...options,
      headers: {
        "Content-Type": "application/json",
        ...options.headers,
      },
      // Sends and accepts the session cookie. The browser stores it as
      // HttpOnly, so no script here ever sees the token.
      credentials: "include",
    });

    if (response.status === 204) {
      return undefined as T;
    }

    const text = await response.text();
    const body = text ? JSON.parse(text) : {};

    if (!response.ok) {
      throw new ApiError(toErrorBody(body, response), response.status);
    }

    return body as T;
  }

  return {
    get: <T>(path: string) => request<T>(path),
    post: <T>(path: string, body?: unknown) =>
      request<T>(path, { method: "POST", body: body ? JSON.stringify(body) : undefined }),
    put: <T>(path: string, body?: unknown) =>
      request<T>(path, { method: "PUT", body: body ? JSON.stringify(body) : undefined }),
    patch: <T>(path: string, body?: unknown) =>
      request<T>(path, { method: "PATCH", body: body ? JSON.stringify(body) : undefined }),
    delete: <T>(path: string) => request<T>(path, { method: "DELETE" }),
  };
}

/**
 * Maps a server response to the shared error shape.
 *
 * The server sends a message and a status; the code is derived here so that
 * shared code has one vocabulary regardless of transport.
 */
function toErrorBody(body: Record<string, unknown>, response: Response): ApiErrorBody {
  const message =
    typeof body.message === "string" ? body.message : "The server could not complete the request.";

  return {
    code: codeForStatus(response.status),
    message,
    requiredPermission: permissionFromMessage(message),
    retryAfterSeconds: retryAfterFrom(response),
  };
}

function codeForStatus(status: number): ErrorCode {
  switch (status) {
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
      return status >= 500 ? "internal" : "invalid";
  }
}

/**
 * Extracts the permission a refusal named, so the interface can tell the user
 * what they would need rather than only that they were refused.
 */
function permissionFromMessage(message: string): string | undefined {
  return message.match(/requires permission "([^"]+)"/)?.[1];
}

function retryAfterFrom(response: Response): number | undefined {
  const header = response.headers.get("Retry-After");
  if (!header) return undefined;
  const seconds = Number.parseInt(header, 10);
  return Number.isFinite(seconds) ? seconds : undefined;
}
