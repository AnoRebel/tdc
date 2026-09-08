import { describe, expect, it } from "vitest";

import { messageLimit, remember, roomLimit, trim, type Remembered } from "./lastseen";

describe("trimming a timeline", () => {
  /*
   * The end, not the beginning.
   *
   * A conversation is read backwards from its most recent message. Keeping the
   * oldest hundred would store precisely the part nobody opens the application
   * to see.
   */
  it("keeps the most recent messages", () => {
    const messages = Array.from({ length: messageLimit + 50 }, (_, i) => i);

    const kept = trim(messages);

    expect(kept).toHaveLength(messageLimit);
    expect(kept.at(-1)).toBe(messageLimit + 49);
    expect(kept[0]).toBe(50);
  });

  it("leaves a short conversation whole", () => {
    expect(trim([1, 2, 3])).toEqual([1, 2, 3]);
  });
});

describe("remembering conversations", () => {
  it("stores a conversation against its room", () => {
    const store = remember({}, "room-1", ["a", "b"], 1000);

    expect(store["room-1"]!.messages).toEqual(["a", "b"]);
    expect(store["room-1"]!.at).toBe(1000);
  });

  it("replaces what was there before", () => {
    const first = remember({}, "room-1", ["a"], 1000);
    const second = remember(first, "room-1", ["a", "b"], 2000);

    expect(second["room-1"]!.messages).toEqual(["a", "b"]);
    expect(second["room-1"]!.at).toBe(2000);
  });

  /*
   * A phone's storage is not unlimited, and neither is the browser's patience
   * with it. Every channel somebody has ever opened is not what they need
   * offline; the handful they actually use is.
   */
  it("drops the least recently seen once there are too many", () => {
    let store: Record<string, Remembered<string>> = {};

    // One more than fits, each seen at a distinct time.
    for (let i = 0; i <= roomLimit; i++) {
      store = remember(store, `room-${i}`, ["message"], 1000 + i);
    }

    expect(Object.keys(store)).toHaveLength(roomLimit);
    // The oldest went; the newest stayed.
    expect(store["room-0"]).toBeUndefined();
    expect(store[`room-${roomLimit}`]).toBeDefined();
  });

  /*
   * Age is when a conversation was last seen, not when it was first stored.
   *
   * Otherwise the room somebody opens every day would be evicted before one
   * they opened once last month, which is exactly backwards.
   */
  it("keeps a conversation that was seen again recently", () => {
    let store: Record<string, Remembered<string>> = {};

    store = remember(store, "daily", ["message"], 1);
    for (let i = 1; i < roomLimit; i++) {
      store = remember(store, `room-${i}`, ["message"], 100 + i);
    }

    // Looked at again, which is what should save it.
    store = remember(store, "daily", ["message"], 10_000);
    store = remember(store, "one-more", ["message"], 10_001);

    expect(Object.keys(store)).toHaveLength(roomLimit);
    expect(store["daily"]).toBeDefined();
    // The oldest of the others went instead.
    expect(store["room-1"]).toBeUndefined();
  });

  it("does not alter the store it was given", () => {
    const store = remember({}, "room-1", ["a"], 1000);
    remember(store, "room-2", ["b"], 2000);

    expect(Object.keys(store)).toEqual(["room-1"]);
  });

  it("trims what it stores", () => {
    const messages = Array.from({ length: messageLimit + 10 }, (_, i) => i);

    const store = remember({}, "room-1", messages, 1000);

    expect(store["room-1"]!.messages).toHaveLength(messageLimit);
  });
});
