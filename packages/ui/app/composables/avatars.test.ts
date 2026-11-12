import { beforeEach, describe, expect, it, vi } from "vitest";

/*
 * Who has a picture, as the faces across the interface ask it.
 *
 * By address, and by username for people on this server, since some lists
 * carry only a username; a person's own new picture shows at once, and so
 * does its removal.
 */

const state = new Map<string, { value: unknown }>();
vi.stubGlobal("useState", (key: string, init: () => unknown) => {
  if (!state.has(key)) state.set(key, { value: init() });
  return state.get(key)!;
});
let fetches = 0;
vi.stubGlobal("useApi", () => ({
  get: async () => {
    fetches++;
    return { avatars: [{ address: "amina@tdc.or.tz", url: "/api/avatars/u1/abc" }] };
  },
}));

const { useAvatars } = await import("./useAvatars");

describe("pictures", () => {
  beforeEach(() => state.clear());

  it("finds somebody by address or, on this server, by username", async () => {
    const avatars = useAvatars();
    await avatars.load();
    expect(avatars.pathOf("amina@tdc.or.tz")).toBe("/api/avatars/u1/abc");
    expect(avatars.pathOf("amina")).toBe("/api/avatars/u1/abc");
    expect(avatars.pathOf("juma@tdc.or.tz")).toBe("");
    expect(avatars.pathOf(undefined)).toBe("");
  });

  it("shows a person's own change at once, and its removal", async () => {
    const avatars = useAvatars();
    await avatars.load();
    avatars.setOwn("juma@tdc.or.tz", "/api/avatars/u2/new");
    expect(avatars.pathOf("juma")).toBe("/api/avatars/u2/new");
    avatars.setOwn("juma@tdc.or.tz", "");
    expect(avatars.pathOf("juma@tdc.or.tz")).toBe("");
    expect(avatars.pathOf("juma")).toBe("");
  });

  it("reads the list again on coming back, only once it is old", async () => {
    vi.useFakeTimers();
    const avatars = useAvatars();
    await avatars.load();
    const before = fetches;
    await avatars.refreshIfStale();
    expect(fetches).toBe(before);
    vi.advanceTimersByTime(5 * 60_000 + 1);
    await avatars.refreshIfStale();
    expect(fetches).toBe(before + 1);
    vi.useRealTimers();
  });
});
