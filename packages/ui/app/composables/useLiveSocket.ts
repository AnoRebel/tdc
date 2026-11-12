/**
 * The live socket, as the two clients open it differently.
 *
 * The reconnection, the backoff, the resubscribing and the replay are the same
 * everywhere and live in useLiveConnection. Opening the connection is not: the
 * web client opens a WebSocket the browser authenticates with its session
 * cookie; the desktop client's session is in the Go jar, out of the webview's
 * reach, so it cannot open one from here — Go opens it and forwards the frames.
 * So only the socket is injected, and the machinery around it is shared.
 *
 * The shape is the small part of WebSocket the connection actually uses: a way
 * to send a message, callbacks for open, message and close, and a way to close.
 * A WebWorker, a Go bridge or a WebSocket all fit it.
 */

/** A live connection in progress, seen the way useLiveConnection needs it. */
export interface LiveSocket {
  /** Sends one JSON-serialisable message to the server. */
  send(message: Record<string, unknown>): void;
  /** Closes the connection. onClose is not called for a deliberate close. */
  close(): void;
}

/** How useLiveConnection hears about a socket it opened. */
export interface LiveSocketHandlers {
  /** The connection is open and ready to send. */
  onOpen(): void;
  /** One frame arrived, as its raw JSON text. */
  onMessage(data: string): void;
  /** The connection dropped. The machinery decides whether to reconnect. */
  onClose(): void;
}

/** Opens a live socket and returns a handle to it. */
export type LiveSocketFactory = (handlers: LiveSocketHandlers) => LiveSocket;

export const liveSocketKey = Symbol("tdc.liveSocket") as InjectionKey<LiveSocketFactory>;

/**
 * How to open a live socket, from whichever application is running.
 *
 * Required rather than defaulted, like the request transport: the layer does
 * not open a WebSocket of its own, so nothing here assumes the web client's
 * way of connecting.
 */
export function useLiveSocket(): LiveSocketFactory {
  const provided = inject(liveSocketKey, null);
  if (!provided) {
    throw new Error(
      "No live socket has been provided. An application extending @tdc/ui must " +
        "provide one under liveSocketKey — see the web client's live plugin.",
    );
  }
  return provided;
}
