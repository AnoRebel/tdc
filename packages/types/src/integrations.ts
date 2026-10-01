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
  kind: "webhook" | "bot";
  /** The webhook or application. */
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
  roomId: string;
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

/** What a subscription may ask for. */
export interface WebhookEventTypes {
  eventTypes: string[];
  payloadVersion: number;
}
