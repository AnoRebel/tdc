/**
 * What this application asks a charting library for.
 *
 * Deliberately small, and deliberately not any one library's vocabulary. The
 * default is TanStack Charts, which is alpha — thirty releases in under a
 * month, breaking changes between minors, and its own documentation asks for
 * exact version pins. A library at that stage is one this application may have
 * to leave, and the cost of leaving should be a configuration change rather
 * than a rewrite of every screen that draws something.
 *
 * So callers describe what they want shown, not how to show it, and an adapter
 * turns that into whichever library is installed.
 */

/** One value in a series, at some position along the horizontal axis. */
export interface ChartPoint {
  /**
   * Where along the axis. A string for a category, a number for a scale, a
   * date for time — an adapter decides how to lay each out.
   */
  at: string | number | Date;
  value: number;
}

/** A named run of points, drawn as one line, one area, or one set of bars. */
export interface ChartSeries {
  /** What to call this in a legend, and what a screen reader announces. */
  name: string;
  points: ChartPoint[];
  /**
   * A colour, or nothing to let the adapter choose from the theme.
   *
   * Naming one here means the caller has a reason — a series that must match
   * something elsewhere on the page. Otherwise the theme should decide, so
   * charts do not drift from the rest of the interface.
   */
  colour?: string;
}

/** How a set of series should be drawn. */
export type ChartKind = "line" | "area" | "bar";

/** Everything needed to draw one chart. */
export interface ChartSpec {
  kind: ChartKind;
  series: ChartSeries[];
  /**
   * What this chart is of, in a sentence.
   *
   * Required rather than optional: a chart is an image to somebody who cannot
   * see it, and one with no description is a blank space in the middle of a
   * page. This is what a screen reader reads.
   */
  describedAs: string;
  /** Labels for the axes, where they are not obvious from the description. */
  axes?: { horizontal?: string; vertical?: string };
  /**
   * Whether the vertical axis must include zero.
   *
   * Defaults to true, because a bar chart that does not start at zero
   * exaggerates every difference in it — which is the most common way a chart
   * misleads without anybody intending it to.
   */
  fromZero?: boolean;
}

/**
 * What an adapter must be able to do.
 *
 * One method. A chart is drawn into an element from a specification, and
 * removed when the element goes — anything more elaborate belongs to the
 * library rather than to this application's idea of a chart.
 */
export interface ChartAdapter {
  /** The name of the implementation, for diagnostics and for tests. */
  readonly name: string;

  /**
   * Draws a chart into an element.
   *
   * Returns how to remove it. A chart that cannot be torn down leaks its
   * listeners and its resize observers, which is the failure that shows up as
   * an application growing slower the longer it is open.
   */
  draw(element: HTMLElement, spec: ChartSpec): ChartHandle;
}

/** A drawn chart, which can be updated or removed. */
export interface ChartHandle {
  /** Redraws with new data, keeping whatever the library set up. */
  update(spec: ChartSpec): void;
  /** Removes the chart and everything it attached. */
  destroy(): void;
}
