/**
 * Authentication and account shapes exchanged with a TDC server.
 */

/** A person on a server, as seen by anyone permitted to see them. */
export interface User {
  id: string;
  username: string;
  displayName: string;
  /**
   * The federated form, user@server. Unambiguous once servers exchange
   * messages, which is why it is carried from the start rather than derived
   * later.
   */
  address: string;
}

/** An authenticated session on one device. */
export interface Session {
  id: string;
  userAgent: string;
  ipAddress?: string;
  createdAt: string;
  lastSeenAt: string;
  expiresAt: string;
  /** True for the session this request was made with. */
  current: boolean;
}

/** What a successful sign-in returns. */
export interface SignInResult {
  user: User;
  /**
   * Also returned in the body for clients that cannot use cookies, such as
   * the desktop application. Browsers rely on the cookie instead.
   */
  token: string;
  expiresAt: string;
}

export interface SignInRequest {
  username: string;
  password: string;
}

export interface RegisterRequest {
  username: string;
  password: string;
  email?: string;
  displayName?: string;
}

/** Whether a server still needs its first administrator. */
export interface SetupStatus {
  setupRequired: boolean;
}

/** What the signed-in user is allowed to do, for adapting the interface. */
export interface CurrentPermissions {
  permissions: string[];
}

/**
 * What signing in through a provider turned out to be.
 *
 * Four of these are answers and four are ways it did not happen. A client shows
 * a different thing for each: a session lands somebody in the application, a
 * link offer asks a question, a new account asks for a name, and the rest are
 * told and returned to the sign-in form.
 */
export type OAuthStatus =
  /** Attached already, so this was a sign-in. */
  | "signed-in"
  /** A provider was added to an account, and a session came with it. */
  | "linked"
  /**
   * A verified address matches an account here. The person is asked whether to
   * attach this identity to it, and proving that account is what accepting
   * means — a verified address at a provider is not a way into an account here.
   */
  | "link-offered"
  /** Nothing matched, so there is an account to create. */
  | "new-account"
  /** They declined at the provider, or changed their mind. */
  | "declined"
  /** The sign-in took too long, or did not come from here. */
  | "expired"
  /** The provider did not complete the exchange. */
  | "failed"
  /** That provider account is already attached to a different account here. */
  | "already-attached"
  /**
   * Nothing has been decided yet.
   *
   * Only ever seen by a client that collects its own sign-in by state — the
   * desktop, asking while somebody is still at the provider. A client holding a
   * handle never sees it: the handle exists only because an outcome was parked.
   *
   * It also covers a state nobody issued, deliberately: a sign-in that was
   * abandoned and one that never existed answer alike.
   */
  | "waiting";

/** Where a sign-in through a provider begins. */
export interface OAuthStart {
  /** Where the person is sent: a browser navigates, the desktop opens it. */
  url: string;
  /** Which sign-in to collect once they come back. */
  state: string;
  expiresAt: string;
}

/** The result of collecting a sign-in through a provider. */
export interface OAuthResult {
  status: OAuthStatus;
  /** Set when the outcome is a session. */
  user?: User;
  token?: string;
  expiresAt?: string;
  /** Where they were going before this started, if anywhere. */
  redirectTo?: string;
}
