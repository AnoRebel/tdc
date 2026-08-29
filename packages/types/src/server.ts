/**
 * Server identity and capability description.
 *
 * A client cannot assume a server offers any particular capability: servers are
 * independently hosted and each operator decides what their deployment does.
 * Clients read this description and adapt, rather than hard-coding assumptions.
 */

/** Semantic version string, e.g. "0.1.0" or "1.0.0-rc.1". */
export type Version = string;

/**
 * What a server tells an unauthenticated caller about itself.
 *
 * Deliberately small, and safe to serve without credentials: a client needs it
 * before it can sign in, and a federated peer needs it before it will exchange
 * anything.
 */
export interface ServerInfo {
  /** Operator-chosen display name. */
  name: string;
  /** Short description of who the server is for. */
  description: string;
  /**
   * The address by which this server is canonically known, set by the operator
   * rather than inferred from the request, so it stays stable behind proxies.
   */
  canonicalAddress: string;
  /** Release version of the running software. */
  version: Version;
  /**
   * Federation protocol versions this server can speak. Empty when federation
   * is disabled, which is the default.
   */
  protocolVersions: string[];
  /** Optional capabilities the operator has enabled. */
  capabilities: Capability[];
  /** Default interface language for users who have not chosen one. */
  defaultLanguage: string;
}

/**
 * An optional capability a server may or may not offer.
 *
 * Every capability here is one an operator can switch off, so clients must
 * treat each as absent until the server says otherwise.
 */
export type Capability =
  | "federation"
  | "voice-video"
  | "webhooks"
  | "applications"
  | "interactive-messages"
  | "link-previews"
  | "web-push"
  | "disposable-chats"
  | "directory"
  | "projects";

/** Health of a running server, for operators and uptime checks. */
export interface HealthStatus {
  status: "ok" | "degraded" | "unavailable";
  /** Per-subsystem detail, e.g. { database: "ok" }. */
  checks: Record<string, "ok" | "degraded" | "unavailable">;
}
