import type { ChartAdapter, ChartHandle, ChartPoint, ChartSpec } from "@tdc/types";

/**
 * A chart adapter that needs nothing.
 *
 * Plain SVG, no library. It exists for two reasons, and only one of them is
 * about drawing.
 *
 * The first is that the default is alpha software with breaking changes
 * between minors. An application whose only charting is a dependency at that
 * stage has no answer when the dependency moves, and this is the answer: a
 * fallback that is always installable because it is already here.
 *
 * The second is that it holds the interface honest. An adapter interface with
 * one implementation is a description of that implementation wearing a more
 * general name — a second one is what proves a caller never had to know.
 */
export function svgCharts(): ChartAdapter {
  return {
    name: "svg",

    draw(element: HTMLElement, spec: ChartSpec): ChartHandle {
      const svg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
      element.append(svg);

      let showing = spec;
      const render = () => {
        svg.replaceChildren();
        paint(svg, showing);
      };

      render();

      /*
       * Redrawn when the element resizes.
       *
       * A chart drawn once at whatever width the page happened to be is one
       * that is wrong on every rotation and every window drag.
       */
      const watching = new ResizeObserver(() => render());
      watching.observe(element);

      return {
        update: (next) => {
          showing = next;
          render();
        },
        destroy: () => {
          // Both, in this order: an observer left watching a removed element
          // is exactly the leak this interface exists to prevent.
          watching.disconnect();
          svg.remove();
        },
      };
    },
  };
}

/** The drawable area, leaving room for the axes to have labels. */
const padding = { top: 8, right: 8, bottom: 24, left: 40 };

function paint(svg: SVGSVGElement, spec: ChartSpec) {
  const width = svg.parentElement?.clientWidth || 320;
  const height = svg.parentElement?.clientHeight || 180;

  svg.setAttribute("viewBox", `0 0 ${width} ${height}`);
  svg.setAttribute("width", "100%");
  svg.setAttribute("height", "100%");

  /*
   * The chart is an image, and what it is of is its alternative text.
   *
   * role="img" with a label, rather than leaving a screen reader to describe
   * a pile of path elements — which it will do, at length, and uselessly.
   */
  svg.setAttribute("role", "img");
  svg.setAttribute("aria-label", spec.describedAs);

  const values = spec.series.flatMap((series) => series.points.map((point) => point.value));
  if (!values.length) return;

  /*
   * Zero is included unless the caller said otherwise, because a chart that
   * starts at its smallest value exaggerates every difference in it.
   */
  const fromZero = spec.fromZero ?? true;
  const lowest = fromZero ? Math.min(0, ...values) : Math.min(...values);
  const highest = Math.max(...values);
  // A flat series still needs a range, or every point lands on one line.
  const span = highest - lowest || 1;

  const plotWidth = width - padding.left - padding.right;
  const plotHeight = height - padding.top - padding.bottom;

  const positionOf = (points: ChartPoint[], index: number) =>
    padding.left +
    (points.length === 1 ? plotWidth / 2 : (index / (points.length - 1)) * plotWidth);

  const heightOf = (value: number) =>
    padding.top + plotHeight - ((value - lowest) / span) * plotHeight;

  spec.series.forEach((series, seriesIndex) => {
    const colour = series.colour ?? `var(--chart-${(seriesIndex % 4) + 1}, currentColor)`;

    if (spec.kind === "bar") {
      const barWidth = (plotWidth / series.points.length) * 0.7;

      for (const [index, point] of series.points.entries()) {
        const bar = document.createElementNS("http://www.w3.org/2000/svg", "rect");
        bar.setAttribute("x", String(positionOf(series.points, index) - barWidth / 2));
        bar.setAttribute("y", String(heightOf(point.value)));
        bar.setAttribute("width", String(barWidth));
        bar.setAttribute("height", String(Math.max(0, heightOf(lowest) - heightOf(point.value))));
        bar.setAttribute("fill", colour);
        svg.append(bar);
      }
      return;
    }

    const path = series.points
      .map(
        (point, index) =>
          `${index ? "L" : "M"}${positionOf(series.points, index)},${heightOf(point.value)}`,
      )
      .join(" ");

    if (spec.kind === "area") {
      const area = document.createElementNS("http://www.w3.org/2000/svg", "path");
      const base = heightOf(lowest);
      area.setAttribute(
        "d",
        `${path} L${positionOf(series.points, series.points.length - 1)},${base} ` +
          `L${positionOf(series.points, 0)},${base} Z`,
      );
      area.setAttribute("fill", colour);
      area.setAttribute("opacity", "0.15");
      svg.append(area);
    }

    const line = document.createElementNS("http://www.w3.org/2000/svg", "path");
    line.setAttribute("d", path);
    line.setAttribute("fill", "none");
    line.setAttribute("stroke", colour);
    line.setAttribute("stroke-width", "2");
    line.setAttribute("stroke-linejoin", "round");
    svg.append(line);
  });
}
