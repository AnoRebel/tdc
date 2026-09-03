import type { LiveEventPayload, LiveFrame } from "@tdc/types";

/**
 * The live connection to a TDC server.
 *
 * Reconnects on its own, resubscribes to what it was watching, and asks for
 * exactly the events it missed. The caller sees a stream of events and a
 * connection state; it does not have to know a socket closed.
 */

/** How the connection currently stands, for showing the person. */
export type ConnectionState = "connecting" | "connected" | "reconnecting" | "offline";

interface LiveHandlers {
  /** Called for each event, live or replayed. */
  onEvent: (roomId: string, sequence: number, payload: LiveEventPayload) => void;
  /**
   * Called when the server says the gap is too large to replay. The caller
   * must refetch the timeline: continuing would leave a hole.
   */
  onResync: (roomId: string) => void;
}

// Backoff bounds. The first retry is quick, because most disconnections are
// momentary; the ceiling keeps a client that has been offline for hours from
// hammering a server that has just come back.
const firstRetryDelay = 500;
const maxRetryDelay = 30_000;

export function useLiveConnection(handlers: LiveHandlers) {
  const state = ref<ConnectionState>("connecting");

  let socket: WebSocket | null = null;
  let retryTimer: ReturnType<typeof setTimeout> | null = null;
  let attempt = 0;
  let closedDeliberately = false;

  // What the connection is watching, and how far each room has been read, so
  // a reconnection can resubscribe and ask for exactly what it missed.
  const subscribed = new Set<string>();
  const lastSequence = new Map<string, number>();

  const apiBase = useRuntimeConfig().public.apiBase;

  function socketUrl(): string {
    const base = apiBase || window.location.origin;
    return base.replace(/^http/, "ws") + "/api/live";
  }

  /**
   * How long to wait before the next attempt.
   *
   * Exponential, with jitter: without it every client disconnected by the same
   * server restart would reconnect in the same instant and knock it over
   * again.
   */
  function retryDelay(): number {
    const ceiling = Math.min(firstRetryDelay * 2 ** attempt, maxRetryDelay);
    return Math.round(ceiling * (0.5 + Math.random() * 0.5));
  }

  function connect() {
    closedDeliberately = false;
    socket = new WebSocket(socketUrl());

    socket.onopen = () => {
      attempt = 0;
      state.value = "connected";
      // Resubscribe to everything, telling the server how far each room was
      // read so it can replay the gap.
      if (subscribed.size > 0) {
        send({
          action: "subscribe",
          rooms: [...subscribed],
          since: Object.fromEntries(lastSequence),
        });
      }
    };

    socket.onmessage = (message) => {
      let frame: LiveFrame;
      try {
        frame = JSON.parse(message.data as string);
      } catch {
        return;
      }
      handleFrame(frame);
    };

    socket.onclose = () => {
      socket = null;
      if (closedDeliberately) {
        state.value = "offline";
        return;
      }
      scheduleReconnect();
    };

    socket.onerror = () => {
      // onclose follows, which is where reconnection is handled.
      socket?.close();
    };
  }

  function handleFrame(frame: LiveFrame) {
    switch (frame.type) {
      case "event": {
        if (!frame.roomId || typeof frame.sequence !== "number") return;
        const payload = frame.payload as LiveEventPayload | undefined;
        if (!payload) return;

        // Recorded before handing on, so a reconnection asks from the right
        // point even if the handler throws.
        lastSequence.set(frame.roomId, frame.sequence);
        handlers.onEvent(frame.roomId, frame.sequence, payload);
        break;
      }

      case "resync": {
        if (!frame.roomId) return;
        // Too far behind to replay. The position is cleared so the next
        // subscription does not ask for a gap the server will not serve.
        lastSequence.delete(frame.roomId);
        handlers.onResync(frame.roomId);
        break;
      }

      case "ready":
      case "subscribed":
      case "pong":
      case "error":
        break;
    }
  }

  function scheduleReconnect() {
    state.value = "reconnecting";
    const delay = retryDelay();
    attempt += 1;
    retryTimer = setTimeout(connect, delay);
  }

  function send(message: Record<string, unknown>) {
    if (socket?.readyState === WebSocket.OPEN) {
      socket.send(JSON.stringify(message));
    }
  }

  /** Watch a room, replaying from a known position if there is one. */
  function subscribe(roomId: string, fromSequence?: number) {
    subscribed.add(roomId);
    if (typeof fromSequence === "number") {
      lastSequence.set(roomId, fromSequence);
    }
    send({
      action: "subscribe",
      rooms: [roomId],
      since: Object.fromEntries(lastSequence),
    });
  }

  function unsubscribe(roomId: string) {
    subscribed.delete(roomId);
    lastSequence.delete(roomId);
    send({ action: "unsubscribe", rooms: [roomId] });
  }

  /** Record how far a room has been read, so a reconnection resumes correctly. */
  function markRead(roomId: string, sequence: number) {
    const current = lastSequence.get(roomId) ?? 0;
    if (sequence > current) {
      lastSequence.set(roomId, sequence);
    }
  }

  function close() {
    closedDeliberately = true;
    if (retryTimer) clearTimeout(retryTimer);
    socket?.close();
    socket = null;
    state.value = "offline";
  }

  onMounted(connect);
  onBeforeUnmount(close);

  return { state: readonly(state), subscribe, unsubscribe, markRead, close };
}
