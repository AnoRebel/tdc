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
