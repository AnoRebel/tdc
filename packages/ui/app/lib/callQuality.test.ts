import { describe, expect, it } from "vitest";

import { encodingFor, initialQuality, stepQuality, type QualityState } from "./callQuality";

function feed(state: QualityState, kbps: number | null, times: number, limited = false) {
  let next = state;
  for (let i = 0; i < times; i++) next = stepQuality(next, { kbps, limited });
  return next;
}

describe("stepping a call down and back up", () => {
  it("gives up video before sound, a step at a time, and never past sound alone", () => {
    let state = feed(initialQuality, 300, 1);
    expect(state.level).toBe(0); // One short reading is not a trend.
    state = feed(state, 300, 1);
    expect(state.level).toBe(1);
    state = feed(state, 30, 2);
    expect(state.level).toBe(2);
    state = feed(state, 30, 20);
    expect(state.level).toBe(4);
    expect(encodingFor(4, "audio").maxBitrate).toBe(24_000);
    expect(encodingFor(3, "audio").maxBitrate).toBeUndefined();
  });

  it("comes back only once the connection has stayed comfortable", () => {
    let state = feed(initialQuality, 30, 20);
    expect(state.level).toBe(4);
    state = feed(state, 2000, 4);
    expect(state.level).toBe(4);
    state = feed(state, 2000, 1);
    expect(state.level).toBe(3);
    state = feed(state, 2000, 20);
    expect(state.level).toBe(0);
  });

  it("believes the browser when it says bandwidth is what holds video back", () => {
    expect(feed(initialQuality, null, 2, true).level).toBe(1);
    expect(feed(initialQuality, 5000, 2, true).level).toBe(1);
    expect(feed(initialQuality, null, 10).level).toBe(0);
  });

  it("pays no mind to a low estimate while the camera is off by choice", () => {
    let state = initialQuality;
    for (let i = 0; i < 10; i++)
      state = stepQuality(state, { kbps: 40, limited: false, video: false });
    expect(state.level).toBe(0);
  });
});
