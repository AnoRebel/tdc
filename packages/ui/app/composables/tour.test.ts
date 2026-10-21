// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";

/*
 * Where "has this person seen it" is answered from.
 *
 * The record lives on the account, so an introduction given on the web client
 * is not given again on the desktop one. This browser keeps a copy that
 * answers immediately — a tour dismissed a moment ago should not reappear
 * while a request is in flight — and the account's copy wins once it arrives.
 */

const state = new Map<string, { value: unknown }>();
const requests: { method: string; path: string; body?: unknown }[] = [];

let storedOnServer: unknown = {};
let serverReachable = true;

vi.stubGlobal("useState", (key: string, init: () => unknown) => {
  if (!state.has(key)) state.set(key, { value: init() });
  return state.get(key)!;
});
vi.stubGlobal("computed", (fn: () => unknown) => ({
  get value() {
    return fn();
  },
}));
vi.stubGlobal("useAuth", () => ({ user: { value: { id: "person-1" } } }));
vi.stubGlobal("useI18n", () => ({ $ts: (key: string) => key }));
vi.stubGlobal("useApi", () => ({
  async get(path: string) {
    requests.push({ method: "GET", path });
    if (!serverReachable) throw new Error("offline");
    return storedOnServer;
  },
  async put(path: string, body: unknown) {
    requests.push({ method: "PUT", path, body });
    if (!serverReachable) throw new Error("offline");
    storedOnServer = body;
    return body;
  },
}));

const { useTour } = await import("./useTour");

describe("remembering which introductions were given", () => {
  beforeEach(() => {
    state.clear();
    requests.length = 0;
    storedOnServer = {};
    serverReachable = true;
    localStorage.clear();
  });

  it("does not offer a tour the account says was seen", async () => {
    // Given on another client, and never in this browser.
    storedOnServer = { seen: ["chat"], reached: {} };

    expect(await useTour().offered("chat")).toBe(false);
  });

  it("offers one the account has no record of", async () => {
    expect(await useTour().offered("chat")).toBe(true);
  });

  /*
   * The whole point of moving this off the browser. Before, a fresh browser
   * meant a fresh record, so the same person met the same introduction on
   * every device they signed in from.
   */
  it("asks the account rather than only this browser", async () => {
    storedOnServer = { seen: ["chat"], reached: {} };

    await useTour().offered("chat");

    expect(requests.some((r) => r.path.includes("/preferences/tours"))).toBe(true);
  });

  /* Once, not per screen that might give one. */
  it("asks only once", async () => {
    const tour = useTour();
    await tour.offered("chat");
    await tour.offered("chat");

    expect(requests.filter((r) => r.method === "GET")).toHaveLength(1);
  });

  /*
   * A server that cannot be reached leaves this working as it did before any
   * of this existed: the browser's own copy answers.
   */
  it("falls back to this browser when the account cannot be reached", async () => {
    serverReachable = false;
    localStorage.setItem("tdc.tours.person-1", JSON.stringify({ seen: ["chat"], reached: {} }));

    expect(await useTour().offered("chat")).toBe(false);
  });

  it("sends a forgotten tour to the account", async () => {
    storedOnServer = { seen: ["chat"], reached: { chat: 2 } };
    const tour = useTour();
    await tour.offered("chat");

    tour.forget("chat");

    const written = requests.find((r) => r.method === "PUT");
    expect(written, "asking to see a tour again reached only this device").toBeDefined();
    expect((written?.body as { seen: string[] }).seen).not.toContain("chat");
  });

  /* And it is offered again afterwards, which is what forgetting is for. */
  it("offers a forgotten tour again", async () => {
    storedOnServer = { seen: ["chat"], reached: {} };
    const tour = useTour();
    expect(await tour.offered("chat")).toBe(false);

    tour.forget("chat");

    expect(await tour.offered("chat")).toBe(true);
  });
});
