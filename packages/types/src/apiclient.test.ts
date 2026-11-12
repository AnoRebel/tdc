import { describe, expect, it, vi } from "vitest";

import { createApiClient } from "./apiclient";
import type { ApiRequest, ApiResponse } from "./client";
import { ApiError } from "./errors";

/*
 * The contract both clients are held to.
 *
 * Run with no framework at all — no Nuxt, no browser, no server. That is the
 * point: the desktop client has no Nuxt runtime, so a contract that could only
 * be exercised inside one would be a contract only one of the two clients ever
 * had to meet.
 *
 * The same tests run against the Wails adapter in 9.10. If they need changing
 * to make that pass, the interface was wrong rather than the adapter.
 */

/**
 * A transport that answers with whatever a test hands it.
 *
 * No status is supplied by default. A default would mean the case that matters
 * most here — a transport with no statuses at all, which is what Wails
 * bindings are — could never be expressed, and the test for it would pass
 * against a status it never meant to have.
 */
function answering(response: Partial<ApiResponse>) {
  const sent: ApiRequest[] = [];
  const send = vi.fn(async (request: ApiRequest) => {
    sent.push(request);
    return { ok: true, ...response } as ApiResponse;
  });

  return { send, sent };
}

describe("what a transport is asked to send", () => {
  it("passes the method and path through unchanged", async () => {
    const { send, sent } = answering({ body: {} });
    const client = createApiClient(send);

    await client.get("/api/me");
    await client.delete("/api/me/sessions/1");

    expect(sent).toEqual([
      { method: "GET", path: "/api/me", body: undefined },
      { method: "DELETE", path: "/api/me/sessions/1", body: undefined },
    ]);
  });

  /*
   * The body is handed over as it was given, not encoded.
   *
   * Encoding is a transport's business: fetch wants a JSON string, and a Wails
   * binding wants an object it can marshal itself. Serialising here would make
   * the desktop adapter parse a string this had just produced.
   */
  it("hands the body over without encoding it", async () => {
    const { send, sent } = answering({ body: {} });
    const client = createApiClient(send);

    await client.post("/api/rooms", { slug: "general", name: "General" });

    expect(sent[0]!.body).toEqual({ slug: "general", name: "General" });
  });

  it("distinguishes no body from an empty one", async () => {
    const { send, sent } = answering({ body: {} });
    const client = createApiClient(send);

    await client.post("/api/auth/signout");
    await client.post("/api/rooms", {});

    expect(sent[0]!.body).toBeUndefined();
    expect(sent[1]!.body).toEqual({});
  });
});

describe("what comes back", () => {
  it("returns the body of a successful response", async () => {
    const { send } = answering({ status: 200, body: { username: "amina" } });

    await expect(createApiClient(send).get("/api/me")).resolves.toEqual({ username: "amina" });
  });

  /*
   * A response that carried nothing is not a failure.
   *
   * A 204 means the request succeeded and had nothing to say, which several
   * endpoints do. Treating it as an error would break every one of them.
   */
  it("returns nothing for a response that carried nothing", async () => {
    const { send } = answering({ status: 204, body: undefined });

    await expect(createApiClient(send).delete("/api/me/sessions/1")).resolves.toBeUndefined();
  });
});

describe("how a refusal is reported", () => {
  /*
   * Every failure arrives as one shape, whatever the transport.
   *
   * Callers branch on a code. A caller that had to know whether it was talking
   * to fetch or to a binding would be two callers with one name.
   */
  it("throws the shared error type", async () => {
    const { send } = answering({ ok: false, status: 403, body: { message: "No." } });

    await expect(createApiClient(send).get("/api/admin")).rejects.toBeInstanceOf(ApiError);
  });

  it("derives a code from the status where there is one", async () => {
    const cases: [number, string][] = [
      [400, "invalid"],
      [401, "unauthenticated"],
      [403, "forbidden"],
      [404, "not-found"],
      [409, "conflict"],
      [429, "rate-limited"],
      [503, "unavailable"],
      [500, "internal"],
      [502, "internal"],
    ];

    for (const [status, code] of cases) {
      const { send } = answering({ ok: false, status, body: {} });

      await expect(createApiClient(send).get("/x")).rejects.toMatchObject({ code });
    }
  });

  /*
   * A transport with no statuses says the code itself.
   *
   * Wails bindings reject with an error and nothing resembling HTTP. The
   * adapter knows what went wrong; this cannot infer it, so the body is
   * believed when it carries a code this application recognises.
   */
  it("believes a code the transport supplied", async () => {
    const { send } = answering({ ok: false, body: { code: "not-enabled", message: "Off." } });

    await expect(createApiClient(send).get("/x")).rejects.toMatchObject({
      code: "not-enabled",
      message: "Off.",
    });
  });

  it("ignores a code it does not recognise", async () => {
    const { send } = answering({ ok: false, status: 403, body: { code: "banana" } });

    // Falls back to the status rather than passing through something no
    // caller has a branch for.
    await expect(createApiClient(send).get("/x")).rejects.toMatchObject({ code: "forbidden" });
  });

  /*
   * A refusal with neither status nor code is treated as the server failing.
   *
   * The safer of the two readings: something internal invites a retry, where
   * "invalid" tells somebody their request was wrong when nothing here knows
   * that.
   */
  it("treats an unexplained refusal as the server's fault", async () => {
    const { send } = answering({ ok: false, body: {} });

    await expect(createApiClient(send).get("/x")).rejects.toMatchObject({ code: "internal" });
  });

  it("keeps the server's own words", async () => {
    const { send } = answering({
      ok: false,
      status: 409,
      body: { message: "That name is already taken." },
    });

    await expect(createApiClient(send).post("/api/rooms", {})).rejects.toMatchObject({
      message: "That name is already taken.",
    });
  });

  it("says something when the server said nothing", async () => {
    const { send } = answering({ ok: false, status: 500, body: {} });

    await expect(createApiClient(send).get("/x")).rejects.toMatchObject({
      message: "The server could not complete the request.",
    });
  });

  /*
   * A refusal that named a permission says which.
   *
   * The difference between telling somebody they cannot do this and telling
   * them what they would need, which is the difference between a dead end and
   * a next step.
   */
  it("carries the permission a refusal named", async () => {
    const { send } = answering({
      ok: false,
      status: 403,
      body: { message: 'That requires permission "moderation.review".' },
    });

    await expect(createApiClient(send).get("/x")).rejects.toMatchObject({
      requiredPermission: "moderation.review",
    });
  });

  it("carries how long to wait when the server said", async () => {
    const { send } = answering({ ok: false, status: 429, body: {}, retryAfterSeconds: 30 });

    await expect(createApiClient(send).post("/x", {})).rejects.toMatchObject({
      code: "rate-limited",
      retryAfterSeconds: 30,
    });
  });
});
