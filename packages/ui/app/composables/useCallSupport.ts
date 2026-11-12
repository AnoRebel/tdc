/**
 * What the application knows about this machine that a page cannot see for
 * itself, when calls cannot work on it — the desktop's GStreamer plugins on
 * Linux, say. Resolves the reason, or an empty string when nothing is known
 * missing. Provided by the desktop application; a browser needs none.
 */
export type CallSupportCheck = () => Promise<string>;

export const callSupportKey = Symbol("tdc.callSupport") as InjectionKey<CallSupportCheck>;

/** Why this device cannot take part in a call, as a reason the interface words. */
export type CallUnsupported =
  "insecure" | "no-webrtc" | "no-webrtc-desktop" | "no-media" | "gstreamer-webrtc";
