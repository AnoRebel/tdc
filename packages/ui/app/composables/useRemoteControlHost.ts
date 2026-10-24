/**
 * Putting a controller's input into this computer, where the application can.
 *
 * A browser cannot move the pointer or press a key outside its own page, so
 * the web client provides nothing here, and somebody sharing from a browser is
 * never offered to anyone as controllable. The desktop application provides
 * it through Go, which keeps the rules: nothing goes in without a grant the
 * sharer gave, for one person, while a whole screen is shared.
 */

/** One piece of a controller's input. Positions are fractions of the shared screen. */
export interface ControlInput {
  kind: "move" | "button" | "wheel" | "key";
  x: number;
  y: number;
  button: number;
  down: boolean;
  dx: number;
  dy: number;
  code: string;
}

/** Why this computer cannot be controlled, when it cannot. */
export type ControlRefusal =
  "browser" | "not-screen" | "wayland" | "no-display" | "no-xtest" | "accessibility" | "platform";

export interface ControlCapability {
  available: boolean;
  reason?: string;
  /** The key that takes control back, as the platform names it. */
  revokeKey: string;
}

export interface ControlState {
  sharing: boolean;
  controller?: string;
  name?: string;
}

export interface RemoteControlHost {
  capability(): Promise<ControlCapability>;
  beginShare(width: number, height: number): Promise<void>;
  endShare(): Promise<void>;
  grant(controller: string, name: string, notice: string): Promise<void>;
  revoke(): Promise<void>;
  apply(controller: string, input: ControlInput): Promise<void>;
  /** Told whenever who controls the screen changes, including by the revoke key. */
  onChange(listener: (state: ControlState) => void): () => void;
}

export const remoteControlKey = Symbol("tdc.remoteControl") as InjectionKey<RemoteControlHost>;

/** The way in, or null where there is none — every browser. */
export function useRemoteControlHost(): RemoteControlHost | null {
  return inject(remoteControlKey, null);
}
