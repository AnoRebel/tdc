// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";

/*
 * What the live connection does with the socket it was given.
 *
 * The socket itself is injected — a WebSocket on the web, a Go bridge on the
 * desktop — so this exercises the machinery around it with a stand-in, which is
 * the only way to test reconnection without a server that can be made to drop.
 */

interface Handlers {
  onOpen(): void;
  onMessage(data: string): void;
  onClose(): void;
}

/** Every socket the machinery has asked for, and what became of each. */
const opened: { handlers: Handlers; closed: boolean; sent: unknown[] }[] = [];

let mounted: (() => void) | null = null;

vi.stubGlobal("ref", <T>(value: T) => ({ value }));
vi.stubGlobal("readonly", <T>(value: T) => value);
vi.stubGlobal("onMounted", (fn: () => void) => {
  mounted = fn;
});
vi.stubGlobal("onBeforeUnmount", () => {});
vi.stubGlobal("useLiveSocket", () => (handlers: Handlers) => {
  const record = { handlers, closed: false, sent: [] as unknown[] };
  opened.push(record);
  return {
    send: (message: unknown) => record.sent.push(message),
    close: () => {
      record.closed = true;
    },
  };
});

const { useLiveConnection } = await import("./useLiveConnection");

beforeEach(() => {
  opened.length = 0;
  mounted = null;
  vi.useFakeTimers();
});

describe("the live connection", () => {
  /*
   * The fault this was written for.
   *
   * A reconnection asked for a new socket and merely forgot the old one. On the
   * web that left its listeners attached; on the desktop the Go side closes the
   * previous connection whenever it opens the next, so the two took turns
   * killing each other and the replay was cut off before a single frame
   * arrived. Forgetting a reference is not closing a connection.
   */
  it("closes the socket it is replacing before opening another", () => {
    useLiveConnection({ onEvent: () => {}, onResync: () => {} });
    mounted?.();

    expect(opened).toHaveLength(1);
    const first = opened[0]!;

    // The connection drops, and the machinery waits out its backoff.
    first.handlers.onClose();
    vi.advanceTimersByTime(60_000);

    expect(opened.length).toBeGreaterThan(1);
    expect(first.closed, "the replaced socket was left open").toBe(true);
  });

  /*
   * Subscriptions are replayed on open rather than sent when asked for.
   *
   * A subscribe sent before the socket reports open would be dropped, and the
   * conversation would show nothing until something else happened to prompt a
   * resubscribe.
   */
  it("replays what it was watching once the socket opens", () => {
    const live = useLiveConnection({ onEvent: () => {}, onResync: () => {} });
    mounted?.();

    const socket = opened[0]!;

    // Asked for before the socket is open, so it cannot be sent yet.
    live.subscribe("room-1", 7);
    expect(socket.sent).toHaveLength(0);

    socket.handlers.onOpen();

    const subscribe = socket.sent.find(
      (message) => (message as { action?: string }).action === "subscribe",
    ) as { rooms: string[]; since: Record<string, number> } | undefined;

    expect(subscribe?.rooms).toContain("room-1");
    expect(subscribe?.since["room-1"]).toBe(7);
  });

  // A frame carries the room and how far it has got; the caller is handed both.
  it("passes an event on with its room and position", () => {
    const seen: { room: string; sequence: number }[] = [];
    useLiveConnection({
      onEvent: (room, sequence) => seen.push({ room, sequence }),
      onResync: () => {},
    });
    mounted?.();

    const socket = opened[0]!;
    socket.handlers.onOpen();
    socket.handlers.onMessage(
      JSON.stringify({ type: "event", roomId: "room-1", sequence: 4, payload: { body: "hello" } }),
    );

    expect(seen).toEqual([{ room: "room-1", sequence: 4 }]);
  });

  // A deliberate close stays closed: no backoff, no new socket.
  it("does not reconnect after it was closed on purpose", () => {
    const live = useLiveConnection({ onEvent: () => {}, onResync: () => {} });
    mounted?.();

    live.close();
    vi.advanceTimersByTime(60_000);

    expect(opened).toHaveLength(1);
  });
});
