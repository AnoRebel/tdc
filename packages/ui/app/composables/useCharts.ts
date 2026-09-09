import type { ChartAdapter } from "@tdc/types";

// Relative, not the layer alias: that alias is how an extending application
// reaches in here, and a file already inside resolves its own neighbours.
import { svgCharts } from "../lib/charts/svg";

/**
 * The key an application provides its charting under.
 *
 * Only where it wants something other than the default. Nothing has to be
 * provided for charts to work, which is the difference between a fallback and
 * a requirement.
 */
export const chartAdapterKey = Symbol("tdc.charts") as InjectionKey<ChartAdapter>;

/**
 * How this application draws a chart.
 *
 * The default is the SVG adapter, which needs nothing and is always there.
 * TanStack Charts is the richer choice and is alpha — thirty releases in under
 * a month, breaking changes between minors, and its own documentation asking
 * for exact pins. An application that wants it provides it; one that does not,
 * or one whose copy has just broken, keeps working.
 *
 * Callers never name either. They describe what they want shown, and swapping
 * the implementation is a line in a plugin rather than a change to any screen.
 */
export function useCharts(): ChartAdapter {
  return inject(chartAdapterKey, null) ?? svgCharts();
}
