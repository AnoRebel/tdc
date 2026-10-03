import { describe, expect, it } from "vitest";

import type { Progress } from "@tdc/types";

import { percentDone, progressFigures } from "./progress";

/*
 * The figures a reader falls back on are the chart's own numbers.
 *
 * When a chart cannot be drawn its table is shown instead, and a table that
 * said anything other than the chart would — rounded differently, a day
 * shifted, a series swapped — would be a second, wrong answer.
 */
describe("progress as a chart and as figures", () => {
  const progress: Progress = {
    total: 6,
    completed: 3,
    from: "2026-10-16",
    to: "2026-10-18",
    trend: [
      { day: "2026-10-16", total: 4, completed: 0 },
      { day: "2026-10-17", total: 6, completed: 2 },
      { day: "2026-10-18", total: 6, completed: 3 },
    ],
  };
  const names = { completed: "Done", total: "All", describedAs: "Issues done each day" };

  it("says in the table exactly what the chart draws", () => {
    const { rows, spec } = progressFigures(progress, names);
    const [done, all] = spec.series;
    expect(done?.name).toBe("Done");
    expect(all?.name).toBe("All");
    expect(rows.map((row) => row.completed)).toEqual(done?.points.map((point) => point.value));
    expect(rows.map((row) => row.total)).toEqual(all?.points.map((point) => point.value));
    expect(rows.map((row) => row.label)).toEqual(done?.points.map((point) => point.at));
    expect(rows.map((row) => row.day)).toEqual(["2026-10-16", "2026-10-17", "2026-10-18"]);
  });

  it("labels each day as the day it is, wherever it is read", () => {
    const original = process.env.TZ;
    try {
      process.env.TZ = "Pacific/Honolulu";
      expect(progressFigures(progress, names).rows.at(-1)?.label).toBe("Sun 18 Oct");
    } finally {
      process.env.TZ = original;
    }
  });

  it("is a describable image", () => {
    expect(progressFigures(progress, names).spec.describedAs).toBe("Issues done each day");
  });

  it("calls nothing to do none done, not a division by zero", () => {
    expect(percentDone(0, 0)).toBe(0);
    expect(percentDone(1, 3)).toBe(33);
    expect(percentDone(3, 6)).toBe(50);
  });
});
