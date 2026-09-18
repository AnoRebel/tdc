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

/** One passkey as the person who enrolled it sees it. */
export interface Passkey {
  id: string;
  /** What they called it, so three of them are distinguishable a year later. */
  label: string;
  /** ISO-8601. */
  added: string;
  /** Absent on one that has never signed anybody in. */
  lastUsed?: string;
}

/**
 * What a sign-in returns when the password was right but is not the whole of it.
 *
 * Distinct from {@link SignInResult}, which carries a session: this carries a
 * ticket, which is worth nothing except the right to try a code for the account
 * it stands for.
 */
export interface SecondFactorRequired {
  secondFactorRequired: true;
  ticket: string;
  expiresAt: string;
  /** So somebody nearly out of recovery codes is warned before they need one. */
  recoveryCodesLeft: number;
}

/** A sign-in either finished, or needs a second factor. */
export type SignInOutcome = SignInResult | SecondFactorRequired;

/** Whether a sign-in stopped short of a session. */
export function needsSecondFactor(outcome: SignInOutcome): outcome is SecondFactorRequired {
  return (outcome as SecondFactorRequired).secondFactorRequired === true;
}

/** What completing a second factor returns: a session, and what it cost. */
export interface SecondFactorResult extends SignInResult {
  /** True when a code from the printed list was spent rather than the app. */
  usedRecoveryCode: boolean;
  recoveryCodesLeft: number;
}

/** Somebody's own second-factor state, for their settings screen. */
export interface SecondFactorStatus {
  /** Whether they have set one up and proved it works. */
  enrolled: boolean;
  /** Whether this server requires one of them, so they are told why. */
  required: boolean;
  recoveryCodesLeft: number;
}

/** What setting up an authenticator app needs. */
export interface SecondFactorEnrolment {
  /** Shown so it can be typed where a camera will not work. */
  secret: string;
  /** The otpauth:// value, which the client renders as a QR code. */
  url: string;
}

/** What a recovery request is answered with — deliberately the same either way. */
export interface RecoveryRequested {
  message: string;
}

/** What redeeming a link returns. Notably not a session. */
export interface RecoveryRedeemed {
  message: string;
}
