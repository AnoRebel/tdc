import { describe, expect, it } from "vitest";

import { lapsesToday, remaining, secondsUntil, secondsUntilMoment } from "./statusexpiry";

// A Wednesday afternoon, in whatever zone the run is in: every expectation
// below is about local wall-clock time, which is what a person means by "today".
const wednesday = new Date(2026, 9, 7, 14, 30, 0);

describe("when a status clears itself", () => {
  it("never, unless asked", () => {
    expect(secondsUntil("never", wednesday)).toBeUndefined();
  });

  it("after the fixed durations", () => {
    expect(secondsUntil("30m", wednesday)).toBe(1800);
    expect(secondsUntil("1h", wednesday)).toBe(3600);
    expect(secondsUntil("4h", wednesday)).toBe(14_400);
  });

  it("at midnight for today", () => {
    // 14:30 to 24:00 is nine and a half hours.
    expect(secondsUntil("today", wednesday)).toBe(9.5 * 3600);
  });

  it("at the end of Sunday for this week", () => {
    // Rest of Wednesday, then Thursday to Sunday whole.
    expect(secondsUntil("week", wednesday)).toBe(9.5 * 3600 + 4 * 86_400);
  });

  it("a week from Monday morning still ends that Sunday, not the next", () => {
    const monday = new Date(2026, 9, 5, 9, 0, 0);
    expect(secondsUntil("week", monday)).toBe(15 * 3600 + 6 * 86_400);
  });

  it("never so soon it lapses before the menu closes", () => {
    const lastMoment = new Date(2026, 9, 7, 23, 59, 50);
    expect(secondsUntil("today", lastMoment)).toBe(60);
  });

  it("refuses a chosen moment that has passed", () => {
    expect(secondsUntilMoment(new Date(2026, 9, 7, 14, 0), wednesday)).toBeUndefined();
    expect(secondsUntilMoment(new Date(2026, 9, 7, 15, 0), wednesday)).toBe(1800);
  });
});

describe("what is left of a status", () => {
  const after = (seconds: number) => new Date(wednesday.getTime() + seconds * 1000);

  it("rounds minutes up, so it never says nothing is left", () => {
    expect(remaining(after(40), wednesday)).toEqual({ unit: "minutes", count: 1 });
    expect(remaining(after(25 * 60), wednesday)).toEqual({ unit: "minutes", count: 25 });
  });

  it("rounds hours and days down, so it never overstates", () => {
    expect(remaining(after(90 * 60), wednesday)).toEqual({ unit: "hours", count: 1 });
    expect(remaining(after(36 * 3600), wednesday)).toEqual({ unit: "days", count: 1 });
  });

  it("is nothing once lapsed", () => {
    expect(remaining(after(-1), wednesday)).toBeUndefined();
    expect(remaining(wednesday, wednesday)).toBeUndefined();
  });

  it("knows whether the end is today", () => {
    expect(lapsesToday(after(3600), wednesday)).toBe(true);
    expect(lapsesToday(after(12 * 3600), wednesday)).toBe(false);
  });
});
