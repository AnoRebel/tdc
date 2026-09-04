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

/**
 * How discoverable a server is to the rest of the network.
 *
 * The same three-way choice a room has, applied one level up: an operator
 * running a server for a particular community decides who may find it at all,
 * and that decision is enforced by the server itself rather than by whatever
 * client happens to be asking.
 */
export type ServerVisibility =
  /** Any connected server may discover it and browse the rooms it publishes. */
  | "public"
  /** Only servers its operator has listed may discover it. */
  | "limited"
  /** Not discoverable; reachable only through an existing explicit connection. */
  | "private";

/**
 * Another server this one is connected to.
 *
 * A person browses these to find rooms beyond their own server. What comes
 * back has already been filtered by each peer's visibility, so a client can
 * present everything it receives without deciding what to hide.
 */
export interface ConnectedServer {
  /** The domain that identifies it, e.g. "vue.tdc.or.tz". */
  domain: string;
  /** Operator-chosen display name. */
  name: string;
  /** Short description of who the server is for. */
  description: string;
  visibility: ServerVisibility;
  /**
   * Whether the connection is currently usable. A server that is listed but
   * unreachable is still shown, so its absence is visible rather than silent.
   */
  reachable: boolean;
  /** How many of its rooms this person may browse, when it publishes any. */
  publishedRoomCount?: number;
}
