import type {
  Call,
  CallDeclined,
  CallLinkUse,
  CallLobby,
  CallReaction,
  CallRing,
  CallSignal,
  LiveEventPayload,
  LiveFrame,
  PrivateReply,
} from "@tdc/types";

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
  /**
   * Called for a program's answer meant for this person alone. Optional,
   * because only a view showing that room has anywhere to put it; it is not
   * part of the room's log, so it has no sequence and is never replayed.
   */
  onPrivate?: (roomId: string, reply: PrivateReply) => void;
  /** Called with who is in a call, whenever that changes. */
  onCallState?: (call: Call) => void;
  /** Called with another participant's call negotiation. */
  onCallSignal?: (signal: CallSignal) => void;
  /** Called when somebody rings this person, and when their ringing should stop. */
  onCallRing?: (ring: CallRing) => void;
  onCallRingEnded?: (call: string) => void;
  /** Called when somebody this person rang declined. */
  onCallDeclined?: (declined: CallDeclined) => void;
  /** Called with who is waiting to be let into a call this person is in. */
  onCallLobby?: (lobby: CallLobby) => void;
  /** Called when this person, waiting in a lobby, is let in or turned away. */
  onCallAdmitted?: (use: CallLinkUse) => void;
  onCallTurnedAway?: (use: CallLinkUse) => void;
  /** Called with a passing reaction somebody in a call sent. */
  onCallReaction?: (reaction: CallReaction) => void;
}

// Backoff bounds. The first retry is quick, because most disconnections are
// momentary; the ceiling keeps a client that has been offline for hours from
// hammering a server that has just come back.
const firstRetryDelay = 500;
const maxRetryDelay = 30_000;

export function useLiveConnection(handlers: LiveHandlers) {
  const state = ref<ConnectionState>("connecting");

  // How the connection is actually opened — a WebSocket on the web, a Go bridge
  // on the desktop. Everything below this is the same for both.
  const openSocket = useLiveSocket();

  let socket: LiveSocket | null = null;
  let open = false;
  let retryTimer: ReturnType<typeof setTimeout> | null = null;
  let attempt = 0;
  let closedDeliberately = false;

  // What the connection is watching, and how far each room has been read, so
  // a reconnection can resubscribe and ask for exactly what it missed.
  const subscribed = new Set<string>();

  // The room currently on screen, restored on reconnection.
  let watching: string | null = null;
  const lastSequence = new Map<string, number>();

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
    open = false;

    /*
     * Whatever came before goes first.
     *
     * A reconnection asks for a new socket, and a socket that is merely
     * forgotten is not closed: on the web its listeners stay attached, and on
     * the desktop the Go side closes the previous connection when it opens the
     * next — so the two sockets take turns killing each other, the replay is
     * cut off mid-flight, and not one frame arrives. Dropping the reference was
     * never the same as letting go of the thing.
     */
    socket?.close();
    socket = null;

    socket = openSocket({
      onOpen: () => {
        open = true;
        attempt = 0;
        state.value = "connected";
        // Resubscribe to everything, telling the server how far each room was
        // read so it can replay the gap.
        if (watching) {
          send({ action: "viewing", room: watching });
        }

        if (subscribed.size > 0) {
          send({
            action: "subscribe",
            rooms: [...subscribed],
            since: Object.fromEntries(lastSequence),
          });
        }
      },

      onMessage: (data) => {
        let frame: LiveFrame;
        try {
          frame = JSON.parse(data);
        } catch {
          return;
        }
        handleFrame(frame);
      },

      onClose: () => {
        /*
         * Let go of it properly, not merely forget it.
         *
         * A socket that reported closed still holds whatever it registered to
         * hear about — listeners on the web, a bridge on the desktop. Nulling
         * the reference leaves those attached, so every reconnection stacked
         * another set on top and the desktop's Go side closed the live
         * connection each time it opened the next. No frame ever survived long
         * enough to be delivered.
         */
        socket?.close();
        socket = null;
        open = false;
        if (closedDeliberately) {
          state.value = "offline";
          return;
        }
        scheduleReconnect();
      },
    });
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

      case "private": {
        if (!frame.roomId || !frame.payload) return;
        handlers.onPrivate?.(frame.roomId, frame.payload as unknown as PrivateReply);
        break;
      }

      case "call.state": {
        if (frame.payload) handlers.onCallState?.(frame.payload as unknown as Call);
        break;
      }

      case "call.signal": {
        if (frame.payload) handlers.onCallSignal?.(frame.payload as unknown as CallSignal);
        break;
      }

      case "call.ring": {
        if (frame.payload) handlers.onCallRing?.(frame.payload as unknown as CallRing);
        break;
      }

      case "call.ring_ended": {
        const ended = frame.payload as unknown as { call?: string } | undefined;
        if (ended?.call) handlers.onCallRingEnded?.(ended.call);
        break;
      }

      case "call.declined": {
        if (frame.payload) handlers.onCallDeclined?.(frame.payload as unknown as CallDeclined);
        break;
      }

      case "call.lobby": {
        if (frame.payload) handlers.onCallLobby?.(frame.payload as unknown as CallLobby);
        break;
      }

      case "call.admitted": {
        if (frame.payload) handlers.onCallAdmitted?.(frame.payload as unknown as CallLinkUse);
        break;
      }

      case "call.turned_away": {
        if (frame.payload) handlers.onCallTurnedAway?.(frame.payload as unknown as CallLinkUse);
        break;
      }

      case "call.reaction": {
        if (frame.payload) handlers.onCallReaction?.(frame.payload as unknown as CallReaction);
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
    // Only once the socket has reported open. Before that the web's WebSocket
    // would throw and the desktop's bridge would have nowhere to send; the
    // machinery replays every subscription on open, so nothing is lost by
    // dropping a message sent too early.
    if (open && socket) {
      socket.send(message);
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
    open = false;
    state.value = "offline";
  }

  onMounted(connect);
  onBeforeUnmount(close);

  /*
   * Tells the server which conversation is on screen.
   *
   * Notifications for it are suppressed while it is: one about a message you
   * just watched arrive is the interruption that teaches people to switch
   * notifications off. Passing null means looking at none, which is what a
   * window losing focus sends — somebody who has tabbed away should hear
   * about it again.
   *
   * Remembered so a reconnection restores it, or a dropped connection would
   * silently start notifying about the conversation still on screen.
   */
  function viewing(roomId: string | null) {
    watching = roomId;
    send({ action: "viewing", room: roomId ?? "" });
  }

  /** Sends one piece of call negotiation to another participant. */
  function signal(call: string, to: string, body: CallSignal["signal"]) {
    send({ action: "signal", call, to, signal: body });
  }

  return { state: readonly(state), subscribe, unsubscribe, markRead, viewing, close, signal };
}
