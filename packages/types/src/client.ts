/*
 * Imported for the {@link} references below, which is a use the linter cannot
 * see: without it the links in this file's documentation resolve to nothing in
 * an editor, and the type somebody most needs to find from here is the one
 * they cannot click through to.
 */
// eslint-disable-next-line @typescript-eslint/no-unused-vars
import type { ApiError } from "./errors";

/**
 * How this application talks to a TDC server.
 *
 * Deliberately small, and deliberately not HTTP. The web client speaks it over
 * fetch; the desktop client speaks it over Wails bindings, which have no
 * status codes, no headers and no URLs in the sense a browser means them. What
 * both have is a path, a body, and either an answer or an {@link ApiError} —
 * so that is the whole of it.
 *
 * Everything above this is written once. A composable that reaches for a
 * Response, a header or a cookie has quietly made itself web-only, and the
 * cost of that shows up in Phase 9 rather than when it was written.
 */
export interface ApiClient {
  get<T>(path: string): Promise<T>;
  post<T>(path: string, body?: unknown): Promise<T>;
  put<T>(path: string, body?: unknown): Promise<T>;
  patch<T>(path: string, body?: unknown): Promise<T>;
  delete<T>(path: string): Promise<T>;
}

/**
 * A request as any transport receives it.
 *
 * The shape an adapter is handed, so the parts common to every transport —
 * deciding a path, encoding a body, normalising a failure — are written once
 * and only the sending differs.
 */
export interface ApiRequest {
  method: "GET" | "POST" | "PUT" | "PATCH" | "DELETE";
  path: string;
  /** Absent for a request with no body, which is not the same as an empty one. */
  body?: unknown;
}

/**
 * What a transport gives back.
 *
 * A status is optional because not every transport has one. Wails bindings
 * reject with an error and nothing else; the code carried by {@link ApiError}
 * is what shared code branches on, and the status is kept only where it exists
 * because it is occasionally the more precise thing to log.
 */
export interface ApiResponse {
  status?: number;
  /** Undefined for a response that carried nothing, such as a 204. */
  body?: unknown;
  /** Whether this is an answer or a refusal, since a transport may say either. */
  ok: boolean;
  /**
   * How long to wait before trying again, when the server said.
   *
   * Carried separately because HTTP puts it in a header and a binding would
   * put it wherever it liked, and the shared code should not know which.
   */
  retryAfterSeconds?: number;
}

/** Sends one request, however this transport sends things. */
export type ApiTransport = (request: ApiRequest) => Promise<ApiResponse>;
