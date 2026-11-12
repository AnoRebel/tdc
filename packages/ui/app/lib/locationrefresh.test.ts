// @vitest-environment jsdom
import type { ApiClient } from "@tdc/types";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { LOCATION_REFRESH_MS, refreshLocation } from "./locationrefresh";

/*
 * The background refresh may never prompt, and asks at most every few hours.
 *
 * Prompting is the failure that matters: somebody who switched detection on
 * agreed to one question in settings, not one each time the window regains
 * focus. So the position is asked for only where permission already stands
 * granted, and only for somebody whose setting is automatic.
 */

function client(mode: "manual" | "automatic") {
  return {
    get: vi.fn(async () => ({ place: "", mode, visibility: "hidden" })),
    post: vi.fn(async () => ({})),
    put: vi.fn(),
    patch: vi.fn(),
    delete: vi.fn(),
  } as unknown as ApiClient & { get: ReturnType<typeof vi.fn>; post: ReturnType<typeof vi.fn> };
}

function device(state: PermissionState) {
  const getCurrentPosition = vi.fn((resolve: PositionCallback) =>
    resolve({ coords: { latitude: -3.37, longitude: 36.68 } } as GeolocationPosition),
  );
  vi.stubGlobal("navigator", {
    geolocation: { getCurrentPosition },
    permissions: { query: vi.fn(async () => ({ state })) },
  });
  return getCurrentPosition;
}

describe("refreshing a detected location", () => {
  beforeEach(() => localStorage.clear());
  afterEach(() => vi.unstubAllGlobals());

  it("never asks for a position without standing permission", async () => {
    for (const state of ["prompt", "denied"] as const) {
      localStorage.clear();
      const ask = device(state);
      const api = client("automatic");
      await refreshLocation(api);
      expect(ask).not.toHaveBeenCalled();
      expect(api.post).not.toHaveBeenCalled();
    }
  });

  it("leaves a typed location alone", async () => {
    const ask = device("granted");
    const api = client("manual");
    await refreshLocation(api);
    expect(ask).not.toHaveBeenCalled();
    expect(api.post).not.toHaveBeenCalled();
  });

  it("sends the position once, then waits", async () => {
    const ask = device("granted");
    const api = client("automatic");
    await refreshLocation(api);
    await refreshLocation(api);
    expect(ask).toHaveBeenCalledTimes(1);
    expect(api.post).toHaveBeenCalledWith("/api/me/location/detect", {
      latitude: -3.37,
      longitude: 36.68,
    });

    localStorage.setItem("tdc.location.checkedAt", String(Date.now() - LOCATION_REFRESH_MS));
    await refreshLocation(api);
    expect(ask).toHaveBeenCalledTimes(2);
  });
});
