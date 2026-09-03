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
