import { createApiClient, ApiError, type ApiClient } from "@tdc/types";
import { describe, expect, it } from "vitest";

import { createHttpTransport } from "./transport";
import { createWailsTransport, Unreachable } from "../../../desktop/frontend/app/lib/transport";

/*
 * The contract both adapters are held to, run against both of them.
 *
 * The tests in packages/types cover what is shared — createApiClient, given a
 * transport that answers however a test likes. These cover the two real
 * transports, which is a different question: whether an HTTP client and a set
 * of Wails bindings, which have nothing in common underneath, present the same
 * behaviour to everything above them.
 *
 * Each case is written once and run twice. A behaviour that needed different
 * expectations per adapter would be one the shared layer above could not rely
 * on — so if one of these needs changing to make an adapter pass, it is the
 * adapter that is wrong.
 */

/** One server's answer, described in terms neither transport knows about. */
interface Answer {
  /** The status, where this transport has statuses. */
  status?: number;
  body?: unknown;
  /** Seconds the server asked us to wait. */
  retryAfter?: number;
  /** Nothing answered at all — the machine is unreachable. */
  unreachable?: boolean;
}

/** What a transport was asked to send, as the test can inspect it. */
interface Sent {
  method: string;
  path: string;
  body?: unknown;
}

/**
 * Builds a client on each adapter, both answering the same way.
 *
 * The stand-ins mimic what each real dependency does rather than what would be
 * convenient: $fetch throws for a refusal and for an unreachable server and is
 * told apart by a status, and the Wails binding returns refusals as answers
 * and rejects only when nothing replied.
 */
function adapters(answer: Answer): { name: string; client: ApiClient; sent: Sent[] }[] {
  const httpSent: Sent[] = [];
  const wailsSent: Sent[] = [];

  const http = createApiClient(
    createHttpTransport(async (path, options) => {
      httpSent.push({ method: options.method, path, body: options.body });

      if (answer.unreachable) {
        // What fetch throws when it never reached anything: no status.
        throw new TypeError("Failed to fetch");
      }

      if (answer.status !== undefined && answer.status >= 400) {
        throw Object.assign(new Error("refused"), {
          status: answer.status,
          data: answer.body,
          response: {
            headers: new Headers(
              answer.retryAfter === undefined ? {} : { "Retry-After": String(answer.retryAfter) },
            ),
          },
        });
      }

      return answer.body;
    }, "http://server.test"),
  );

  const wails = createApiClient(
    createWailsTransport({
      async Send(request) {
        wailsSent.push({ method: request.method, path: request.path, body: request.body });

        if (answer.unreachable) {
          // What Go returns when it could not reach the server: an error,
          // rather than an answer with ok false.
          throw new Error("reaching http://server.test: connection refused");
        }

        const ok = answer.status === undefined || answer.status < 400;
        return {
          ok,
          status: answer.status,
          body: answer.body,
          retryAfterSeconds: answer.retryAfter,
        };
      },
    }),
  );

  return [
    { name: "http", client: http, sent: httpSent },
    { name: "wails", client: wails, sent: wailsSent },
  ];
}

/** Runs one case against both adapters. */
function forEachAdapter(
  answer: Answer,
  check: (adapter: { name: string; client: ApiClient; sent: Sent[] }) => Promise<void>,
) {
  return Promise.all(adapters(answer).map(check));
}

describe("what a transport sends", () => {
  it("carries the method and path unchanged", async () => {
    await forEachAdapter({ body: {} }, async ({ name, client, sent }) => {
      await client.get("/api/rooms");
      await client.delete("/api/rooms/1");

      expect(sent[0], `${name} changed a GET`).toMatchObject({
        method: "GET",
        path: "/api/rooms",
      });
      expect(sent[1], `${name} changed a DELETE`).toMatchObject({
        method: "DELETE",
        path: "/api/rooms/1",
      });
    });
  });

  /*
   * Neither adapter encodes it. $fetch serialises an object itself, and Wails
   * marshals one — so a transport that stringified here would hand the other
   * side a string to parse back into what it already had.
   */
  it("hands the body over without encoding it", async () => {
    const body = { name: "General", nested: { deep: true } };

    await forEachAdapter({ body: {} }, async ({ name, client, sent }) => {
      await client.post("/api/rooms", body);

      expect(sent[0]?.body, `${name} encoded the body`).toEqual(body);
    });
  });

  it("distinguishes no body from an empty one", async () => {
    await forEachAdapter({ body: {} }, async ({ name, client, sent }) => {
      await client.post("/api/rooms/1/join");
      await client.post("/api/rooms/2/join", {});

      expect(sent[0]?.body, `${name} invented a body`).toBeUndefined();
      expect(sent[1]?.body, `${name} discarded an empty body`).toEqual({});
    });
  });
});

describe("what comes back", () => {
  it("returns the body of a successful response", async () => {
    const body = { id: "1", name: "General" };

    await forEachAdapter({ status: 200, body }, async ({ name, client }) => {
      expect(await client.get("/api/rooms/1"), `${name} lost the body`).toEqual(body);
    });
  });

  /* A 204 succeeded and had nothing to say, which is not the same as null. */
  it("returns nothing for a response that carried nothing", async () => {
    await forEachAdapter({ status: 204 }, async ({ name, client }) => {
      expect(await client.delete("/api/rooms/1"), `${name} invented a body`).toBeUndefined();
    });
  });
});

describe("how a refusal is reported", () => {
  it("throws the shared error type", async () => {
    await forEachAdapter({ status: 403, body: { message: "no" } }, async ({ name, client }) => {
      await expect(client.get("/api/rooms"), `${name} threw something else`).rejects.toBeInstanceOf(
        ApiError,
      );
    });
  });

  /*
   * The same code from the same status, whichever transport carried it.
   *
   * This is the whole point of the exercise: the layer above branches on these
   * codes, and a refusal that meant "forbidden" on one client and something
   * else on the other would make that code wrong in one of them.
   */
  it("derives the same code from a status either way", async () => {
    const expected: [number, string][] = [
      [400, "invalid"],
      [401, "unauthenticated"],
      [403, "forbidden"],
      [404, "not-found"],
      [409, "conflict"],
      [429, "rate-limited"],
      [503, "unavailable"],
      [500, "internal"],
    ];

    for (const [status, code] of expected) {
      await forEachAdapter({ status, body: {} }, async ({ name, client }) => {
        await expect(client.get("/api/rooms")).rejects.toMatchObject({ code });
        expect(code, `${name} disagreed about ${status}`).toBe(code);
      });
    }
  });

  it("keeps the server's own words", async () => {
    await forEachAdapter(
      { status: 403, body: { message: "You are not a member of this channel." } },
      async ({ name, client }) => {
        await expect(client.get("/api/rooms/1"), `${name} lost the message`).rejects.toThrow(
          "You are not a member of this channel.",
        );
      },
    );
  });

  it("carries how long to wait when the server said", async () => {
    await forEachAdapter(
      { status: 429, body: { message: "slow down" }, retryAfter: 30 },
      async ({ name, client }) => {
        await expect(client.get("/api/rooms"), `${name} lost the delay`).rejects.toMatchObject({
          retryAfterSeconds: 30,
        });
      },
    );
  });

  /*
   * The permission a refusal named, so the interface can say what somebody
   * would need rather than only that they were refused.
   */
  it("carries the permission a refusal named", async () => {
    await forEachAdapter(
      { status: 403, body: { message: 'This requires permission "channel.create".' } },
      async ({ name, client }) => {
        await expect(
          client.post("/api/rooms"),
          `${name} lost the permission`,
        ).rejects.toMatchObject({ requiredPermission: "channel.create" });
      },
    );
  });
});

/*
 * A server that was never there.
 *
 * Both adapters have to make this distinguishable from a refusal, because the
 * shared layer decides whether to retry or to discard work on exactly that:
 * a refusal is the server's answer and will not change, and unreachability
 * might. Neither throws an ApiError for it — an ApiError means the server
 * answered — and neither carries a status, which is what readFailure reads.
 */
describe("a server that could not be reached", () => {
  it("is not reported as a refusal", async () => {
    await forEachAdapter({ unreachable: true }, async ({ name, client }) => {
      await expect(
        client.get("/api/rooms"),
        `${name} reported an unreachable server as a refusal`,
      ).rejects.not.toBeInstanceOf(ApiError);
    });
  });

  it("carries no status, which is how it is told apart", async () => {
    await forEachAdapter({ unreachable: true }, async ({ name, client }) => {
      const cause = await client.get("/api/rooms").catch((error: unknown) => error);

      expect(
        (cause as { status?: unknown }).status,
        `${name} attached a status to an unreachable server`,
      ).toBeUndefined();
    });
  });

  it("says what happened", async () => {
    await forEachAdapter({ unreachable: true }, async ({ name, client }) => {
      const cause = await client.get("/api/rooms").catch((error: unknown) => error);

      expect(cause, `${name} threw something with no message`).toBeInstanceOf(Error);
      expect((cause as Error).message, `${name} said nothing useful`).not.toBe("");
    });
  });

  /* The desktop one names its own type, so the plugin can tell it apart from a
     programming error without matching on a message. */
  it("is a named type on the desktop client", async () => {
    const [, wails] = adapters({ unreachable: true });
    const cause = await wails!.client.get("/api/rooms").catch((error: unknown) => error);

    expect(cause).toBeInstanceOf(Unreachable);
  });
});
