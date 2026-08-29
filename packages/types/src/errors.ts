/**
 * The error shape every transport produces.
 *
 * The web client talks to the server over HTTP and the desktop client over
 * Wails bindings, which have no status codes. Both normalise failures into this
 * shape so that shared code can handle errors without knowing which transport
 * produced them.
 */

/** Machine-readable error categories. Clients branch on these, not on prose. */
export type ErrorCode =
  | "unauthenticated"
  | "forbidden"
  | "not-found"
  | "conflict"
  | "invalid"
  | "rate-limited"
  | "not-enabled"
  | "unavailable"
  | "internal";

export interface ApiErrorBody {
  code: ErrorCode;
  /** Human-readable message, safe to show to the user. */
  message: string;
  /**
   * The permission that would have allowed this request, when it failed for
   * lack of one. Present so a client can explain the refusal rather than
   * showing a bare "forbidden".
   */
  requiredPermission?: string;
  /** Field-level problems, keyed by field path, for validation failures. */
  fields?: Record<string, string>;
  /** Seconds to wait before retrying, when rate limited. */
  retryAfterSeconds?: number;
}

/**
 * An error raised by any ApiClient implementation.
 *
 * `status` is the HTTP status where one exists and 0 where the transport has no
 * concept of one, so callers should branch on `code` instead.
 */
export class ApiError extends Error {
  readonly code: ErrorCode;
  readonly status: number;
  readonly requiredPermission?: string;
  readonly fields?: Record<string, string>;
  readonly retryAfterSeconds?: number;

  constructor(body: ApiErrorBody, status = 0) {
    super(body.message);
    this.name = "ApiError";
    this.code = body.code;
    this.status = status;
    this.requiredPermission = body.requiredPermission;
    this.fields = body.fields;
    this.retryAfterSeconds = body.retryAfterSeconds;
  }
}

/** Narrowing helper, since instanceof is unreliable across bundle boundaries. */
export function isApiError(value: unknown): value is ApiError {
  return value instanceof Error && value.name === "ApiError";
}
