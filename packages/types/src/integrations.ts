/**
 * Programs that act on a TDC server: the controls their messages carry,
 * webhooks in both directions, and applications.
 *
 * Kept apart from the room types because these describe what a program sends
 * and receives, which a client only renders.
 */

import type { MessageAction } from "./rooms";

/**
 * What sort of control a message offers.
 *
 * Absent means a button, so a message posted before menus existed reads the
 * same. A client meeting a kind it does not know leaves that control out and
 * shows the rest of the message, rather than failing to render it.
 */
export type ControlKind = "select" | "input";

/** One choice in a menu. */
export interface ControlOption {
  /** What a press reports back. */
  value: string;
  /** What a reader sees. */
  label: string;
}

/** Any control a message may carry: a button, a menu, or a short field. */
export interface MessageControl extends MessageAction {
  kind?: ControlKind;
  /** For a menu, what may be chosen. */
  options?: ControlOption[];
  /** For a field, a hint shown while it is empty. */
  placeholder?: string;
  /** For a field, the most that may be typed. */
  maxLength?: number;
  /** A permission the person pressing must hold, which the server enforces. */
  requires?: string;
}

/** The kinds this client knows how to render. Anything else is left out. */
export const renderableControlKinds: ReadonlySet<string | undefined> = new Set([
  undefined,
  "",
  "select",
  "input",
]);

/**
 * The program a message came from, when it was not a person. Set by the
 * server, in the message's own content; absent on everybody's messages.
 */
export interface MessageVia {
  /**
   * "automatic" is an automatic reply: sent as the person, by the server, on
   * their behalf while they are away. The id is theirs and the name is their
   * display name.
   */
  kind: "webhook" | "bot" | "automatic";
  /** The webhook or application, or the person for an automatic reply. */
  id: string;
  /** What to show in place of a person's name. */
  name: string;
}

/** The content of a message.updated event. */
export interface ControlsUpdate {
  /** The message whose text and controls were replaced. */
  updates: string;
  /** The version now current, which a press should name. */
  version: number;
  body: string;
  actions: MessageControl[];
}

/** What a press may carry. */
export interface PressRequest {
  /** The option chosen, or the text typed. Absent for a button. */
  value?: string;
  /** The version of the controls the person was looking at. */
  version?: number;
}

/**
 * A program's answer meant for one person, delivered over the live
 * connection and never kept.
 */
export interface PrivateReply {
  id: string;
  /** The press being answered. */
  press: string;
  /** The message whose control was pressed. */
  message: string;
  sender: string;
  body: string;
  /** Epoch milliseconds, like every other live frame. */
  createdAt: number;
}

/** An outgoing webhook, as its owner sees it. The secret is never here. */
export interface WebhookSubscription {
  id: string;
  label: string;
  url: string;
  /** What it listens to: one channel, one project, or the whole server. */
  scope: WebhookScope;
  /** The channel, or the project's stream; absent for the server. */
  roomId?: string;
  /** Set for a project subscription. */
  projectId?: string;
  eventTypes: string[];
  payloadVersion: number;
  state: "active" | "disabled";
  disabledAt?: string;
  disabledReason?: string;
  /** Failed attempts since the last success. */
  consecutiveFailures: number;
  /** Deliveries held back by the rate limit, and when last. */
  rateLimited: number;
  lastRateLimited?: string;
  /** When a delivery last found its owner unable to read the channel. */
  accessLost?: string;
  created: string;
}

/** The one response carrying a webhook's secret. */
export interface CreatedWebhook extends WebhookSubscription {
  secret: string;
}

export type WebhookDeliveryState =
  "pending" | "delivering" | "delivered" | "abandoned" | "cancelled";

/** One thing a webhook was, or will be, sent. */
export interface WebhookDelivery {
  id: string;
  kind: string;
  event?: string;
  state: WebhookDeliveryState;
  attempts: number;
  nextAttemptAt?: string;
  created: string;
  completed?: string;
}

/** One try at sending a delivery. */
export interface WebhookAttempt {
  id: string;
  delivery: string;
  kind: string;
  attempt: number;
  at: string;
  outcome: "delivered" | "failed" | "rate_limited";
  status?: number;
  error?: string;
  response?: string;
  durationMs: number;
}

/** A webhook with what has recently been tried for it, newest first. */
export interface WebhookHistory extends WebhookSubscription {
  deliveries: WebhookDelivery[];
  attempts: WebhookAttempt[];
}

/** An incoming webhook, as a channel's managers see it. Never the credential. */
export interface IncomingWebhook {
  id: string;
  label: string;
  roomId: string;
  created: string;
  createdBy?: string;
  revoked?: string;
  lastUsed?: string;
  posted: number;
  /** Calls refused by the rate limit, and when last. */
  refused: number;
  lastRefused?: string;
  /** The address its messages appear from. */
  sender: string;
}

/** The one response carrying an incoming webhook's address. */
export interface CreatedIncomingWebhook extends IncomingWebhook {
  /** Where to post. It contains the credential. */
  url: string;
  token: string;
}

/** A channel an application may act in. */
export interface ApplicationChannel {
  id: string;
  name: string;
}

/** An application as an administrator manages it. */
export interface Application {
  id: string;
  name: string;
  purpose: string;
  state: "registered" | "approved" | "removed";
  /** What it asks for, and what it was approved for. */
  requested: string[];
  granted: string[];
  sendsDataExternally: boolean;
  /** What leaves the server and where it goes, when anything does. */
  externalDisclosure?: string;
  registered: string;
  approved?: string;
  /** The account it acts through, once approved. */
  account?: string;
  /** That account's identifier, which moderation acts on. */
  accountId?: string;
  channels: ApplicationChannel[];
  credentials?: ApplicationCredential[];
  rateLimited: number;
  lastRateLimited?: string;
  /** Where presses on its messages are delivered, if anywhere. */
  invocationUrl?: string;
}

/** The answer to setting an invocation address: the secret, shown once. */
export interface InvocationAddress {
  url: string;
  secret?: string;
}

/** One press delivered, or being delivered, to an application's address. */
export interface Invocation {
  id: string;
  eventId?: string;
  type: string;
  state: "pending" | "delivering" | "delivered" | "abandoned" | "cancelled";
  attempts: number;
  created: string;
  completed?: string;
  nextAttempt?: string;
  lastStatus?: number;
  lastError?: string;
}

export interface ApplicationCredential {
  id: string;
  label: string;
  created: string;
  lastUsed?: string;
}

/** The one response carrying an application credential's secret. */
export interface IssuedApplicationCredential {
  id: string;
  label: string;
  token: string;
}

/** An application as anybody on the server sees it. */
export interface InstalledApplication {
  id: string;
  name: string;
  purpose: string;
  permissions: string[];
  approved: string;
  sendsDataExternally: boolean;
  externalDisclosure?: string;
  account?: string;
  channelCount: number;
  /** The host presses on its messages are sent to, if anywhere. */
  invocationHost?: string;
}

/** A permission an application can be granted. */
export interface GrantablePermission {
  name: string;
  description: string;
  /** Whether it is granted per channel, rather than across the server. */
  perChannel: boolean;
  administrative: boolean;
}

/** What a subscription listens to. */
export type WebhookScope = "channel" | "project" | "server";

/** One type a subscription may ask for, as the catalogue describes it. */
export interface WebhookEventType {
  name: string;
  /** What it is about: messages, members, channels, issues, people... */
  group: string;
  /** Where it can be asked for. */
  scopes: WebhookScope[];
  /** A server permission it needs, for an event that belongs to no room. */
  permission?: string;
  /** Named by the server from another event, as a comment is from a message. */
  derived?: boolean;
  /** Whether the caller could ask for it now. */
  available: boolean;
}

/** What a subscription may ask for. */
export interface WebhookEventTypes {
  eventTypes: string[];
  catalogue: WebhookEventType[];
  payloadVersion: number;
}
