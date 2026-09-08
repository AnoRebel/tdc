import { describe, expect, it } from "vitest";

import {
  afterFailure,
  afterSending,
  afterUnsending,
  maxAttempts,
  readFailure,
  sendOrder,
  stillWaiting,
  shouldRetry,
  type Queued,
} from "./outbox";

function queued(overrides: Partial<Queued> = {}): Queued {
  return {
    clientToken: "token-1",
    roomId: "room-1",
    body: "Habari",
    attachmentIds: [],
    composedAt: 1000,
    attempts: 0,
    ...overrides,
  };
}

describe("what a failure means", () => {
  /*
   * The distinction the whole queue rests on.
   *
   * A network that dropped will come back, and the message should wait for it.
   * A server that read the message and said no will say no again, and queueing
   * it means somebody watches their own message retry forever.
   */
  it("treats an unreachable server as worth waiting for", () => {
    // What fetch rejects with when there is nothing to reach: no status,
    // because no response was ever received.
    expect(readFailure(new TypeError("Failed to fetch"))).toBe("unreachable");
    expect(readFailure(new Error("network error"))).toBe("unreachable");
  });

  it("treats a refusal as final", () => {
    for (const status of [400, 403, 404, 409, 422]) {
      expect(readFailure({ status })).toBe("refused");
    }
  });

  /*
   * A server that failed is not a server that refused.
   *
   * A 500 means it read the request and could not complete it — a restart, a
   * database blip, a deploy. The same message a minute later may well work,
   * and discarding it would lose something that was going to send.
   */
  it("treats a server failure as worth waiting for", () => {
    for (const status of [500, 502, 503, 504]) {
      expect(readFailure({ status })).toBe("unreachable");
    }
  });
});

describe("whether to try again", () => {
  it("never retries something the server refused", () => {
    expect(shouldRetry(queued(), "refused")).toBe(false);
  });

  it("retries something it could not reach", () => {
    expect(shouldRetry(queued(), "unreachable")).toBe(true);
  });

  /*
   * A message gives up eventually.
   *
   * Not to spare the server — a retry is idempotent and costs one request —
   * but because somebody deserves to be told a message is not going to send,
   * rather than watching it sit there looking like it might.
   */
  it("gives up after enough attempts", () => {
    expect(shouldRetry(queued({ attempts: maxAttempts - 1 }), "unreachable")).toBe(true);
    expect(shouldRetry(queued({ attempts: maxAttempts }), "unreachable")).toBe(false);
  });
});

describe("the queue after an attempt", () => {
  it("keeps an unreachable message and counts the attempt", () => {
    const after = afterFailure([queued()], "token-1", "unreachable");

    expect(after).toHaveLength(1);
    expect(after[0]!.attempts).toBe(1);
    // The text is untouched, which is the point of keeping it at all.
    expect(after[0]!.body).toBe("Habari");
  });

  it("drops a message the server refused", () => {
    expect(afterFailure([queued()], "token-1", "refused")).toHaveLength(0);
  });

  it("drops a message that has been tried enough", () => {
    const exhausted = queued({ attempts: maxAttempts - 1 });
    expect(afterFailure([exhausted], "token-1", "unreachable")).toHaveLength(0);
  });

  it("leaves the other messages alone", () => {
    const queue = [queued(), queued({ clientToken: "token-2", body: "Asante" })];
    const after = afterFailure(queue, "token-1", "refused");

    expect(after).toHaveLength(1);
    expect(after[0]!.clientToken).toBe("token-2");
    expect(after[0]!.attempts).toBe(0);
  });

  /*
   * A caller holding the queue for a render is not changed underneath it.
   *
   * Mutating in place would move messages out of a list Vue is already
   * displaying, which is how a row disappears mid-scroll.
   */
  it("does not alter the queue it was given", () => {
    const queue = [queued()];
    afterFailure(queue, "token-1", "unreachable");

    expect(queue[0]!.attempts).toBe(0);
  });
});

describe("the queue after a send", () => {
  /*
   * Removed by token, not by position.
   *
   * The queue can be added to while a request is in flight — somebody keeps
   * typing — and removing an index would drop whatever had moved into it.
   */
  it("removes the message that was sent, wherever it now sits", () => {
    const queue = [
      queued({ clientToken: "added-while-sending" }),
      queued({ clientToken: "token-1" }),
    ];

    const after = afterSending(queue, "token-1");

    expect(after).toHaveLength(1);
    expect(after[0]!.clientToken).toBe("added-while-sending");
  });
});

describe("the order things are sent in", () => {
  /*
   * Somebody's own sentences arrive in the order they wrote them.
   *
   * Sending newest first would reorder a train of thought into nonsense, which
   * reads as incoherence rather than as a delay.
   */
  it("sends oldest first", () => {
    const queue = [
      queued({ clientToken: "third", composedAt: 3000 }),
      queued({ clientToken: "first", composedAt: 1000 }),
      queued({ clientToken: "second", composedAt: 2000 }),
    ];

    expect(sendOrder(queue).map((message) => message.clientToken)).toEqual([
      "first",
      "second",
      "third",
    ]);
  });

  it("does not reorder the queue it was given", () => {
    const queue = [
      queued({ clientToken: "third", composedAt: 3000 }),
      queued({ clientToken: "first", composedAt: 1000 }),
    ];

    sendOrder(queue);

    expect(queue[0]!.clientToken).toBe("third");
  });
});

describe("withdrawing a message", () => {
  it("removes the one that was withdrawn", () => {
    const queue = [queued(), queued({ clientToken: "token-2" })];

    const after = afterUnsending(queue, "token-1");

    expect(after).toHaveLength(1);
    expect(after[0]!.clientToken).toBe("token-2");
  });

  it("does not alter the queue it was given", () => {
    const queue = [queued()];
    afterUnsending(queue, "token-1");

    expect(queue).toHaveLength(1);
  });

  /*
   * The race that makes this more than a filter.
   *
   * A flush reads the queue once and sends through it. Somebody who withdraws
   * a message while the one before it is still in flight has to be obeyed —
   * otherwise unsend works only when nothing is happening, which is precisely
   * when nobody needs it.
   */
  it("is noticed by a send that was already under way", () => {
    const queue = [queued({ clientToken: "first" }), queued({ clientToken: "second" })];

    // "first" is in flight; "second" is withdrawn meanwhile.
    const after = afterUnsending(queue, "second");

    expect(stillWaiting(after, "first")).toBe(true);
    expect(stillWaiting(after, "second")).toBe(false);
  });
});
