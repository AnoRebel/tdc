import type { ChartSpec, Progress } from "@tdc/types";

import { onCalendarWeekday } from "./dates";

/** One day of a trend, as both the chart and its table say it. */
export interface ProgressRow {
  day: string;
  /** The day as a reader sees it: "Sun 18 Oct". */
  label: string;
  completed: number;
  total: number;
}

/**
 * A progress report as a chart and as figures, from one source.
 *
 * Both come from the same rows, deliberately: the table a reader falls back on
 * when the chart cannot be drawn must say exactly what the chart would have,
 * and the only way to be sure of that is for there to be nothing in between
 * that could make them differ.
 */
export function progressFigures(
  progress: Progress,
  names: { completed: string; total: string; describedAs: string },
): { rows: ProgressRow[]; spec: ChartSpec } {
  const rows = progress.trend.map((point) => ({
    day: point.day,
    label: onCalendarWeekday(point.day),
    completed: point.completed,
    total: point.total,
  }));
  return {
    rows,
    spec: {
      kind: "line",
      describedAs: names.describedAs,
      series: [
        {
          name: names.completed,
          points: rows.map((row) => ({ at: row.label, value: row.completed })),
        },
        { name: names.total, points: rows.map((row) => ({ at: row.label, value: row.total })) },
      ],
    },
  };
}

/** How much is done, as a whole percentage: 0 when there is nothing to do. */
export function percentDone(completed: number, total: number): number {
  return total === 0 ? 0 : Math.round((completed / total) * 100);
}
