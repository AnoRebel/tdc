/**
 * A DOM for these, since they draw into one.
 *
 * Declared per file rather than for the whole suite: most of these tests are
 * pure decisions that run in milliseconds without a document, and giving every
 * one of them jsdom would make the suite pay for the few that need it.
 *
 * @vitest-environment jsdom
 */
import type { ChartAdapter, ChartHandle, ChartSpec } from "@tdc/types";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { svgCharts } from "./svg";

/*
 * Whether a caller has to know which library is drawing.
 *
 * The default is alpha — thirty releases in under a month, breaking changes
 * between minors — so this application will very likely have to change
 * charting libraries at some point. The interface is only worth having if
 * that change costs nothing above it, and the way to know is to drive two
 * implementations through exactly the same calls.
 */

/** Something for a chart to be drawn into, since jsdom has no layout. */
function anElement(): HTMLElement {
  const element = document.createElement("div");
  // jsdom reports zero for everything, and a chart of zero width draws
  // nothing — so the sizes a real browser would supply are stated here.
  Object.defineProperty(element, "clientWidth", { value: 400, configurable: true });
  Object.defineProperty(element, "clientHeight", { value: 200, configurable: true });
  document.body.append(element);
  return element;
}

/**
 * A second implementation, which exists to prove the first is replaceable.
 *
 * It records what it was asked to do rather than drawing anything. If a caller
 * written against the interface works with this, it was never depending on the
 * other one.
 */
function recordingCharts(): ChartAdapter & { drawn: ChartSpec[]; destroyed: number } {
  const drawn: ChartSpec[] = [];
  let destroyed = 0;

  return {
    name: "recording",
    get drawn() {
      return drawn;
    },
    get destroyed() {
      return destroyed;
    },
    draw(_element: HTMLElement, spec: ChartSpec): ChartHandle {
      drawn.push(spec);
      return {
        update: (next) => drawn.push(next),
        destroy: () => {
          destroyed += 1;
        },
      };
    },
  };
}

const aSpec: ChartSpec = {
  kind: "line",
  describedAs: "Messages sent each day this week",
  series: [
    {
      name: "Messages",
      points: [
        { at: "Mon", value: 12 },
        { at: "Tue", value: 30 },
        { at: "Wed", value: 7 },
      ],
    },
  ],
};

beforeEach(() => {
  document.body.replaceChildren();
  // jsdom has no ResizeObserver, which the adapter uses to redraw on resize.
  vi.stubGlobal(
    "ResizeObserver",
    class {
      observe() {}
      disconnect() {}
    },
  );
});

describe("what a caller has to know", () => {
  /*
   * The requirement 9.7 names. Two implementations, one sequence of calls, no
   * branch anywhere on which is in use.
   */
  it("drives either implementation through the same calls", () => {
    const sequence = (charts: ChartAdapter) => {
      const element = anElement();
      const handle = charts.draw(element, aSpec);
      handle.update({ ...aSpec, kind: "bar" });
      handle.destroy();
      return charts.name;
    };

    // Neither call knows anything about the other, and neither throws.
    expect(sequence(svgCharts())).toBe("svg");
    expect(sequence(recordingCharts())).toBe("recording");
  });

  it("reaches the implementation that was provided", () => {
    const recording = recordingCharts();
    const element = anElement();

    const handle = recording.draw(element, aSpec);
    handle.update({ ...aSpec, kind: "area" });
    handle.destroy();

    expect(recording.drawn.map((spec) => spec.kind)).toEqual(["line", "area"]);
    expect(recording.destroyed).toBe(1);
  });
});

describe("the fallback that needs nothing", () => {
  it("draws something", () => {
    const element = anElement();
    svgCharts().draw(element, aSpec);

    const svg = element.querySelector("svg");
    expect(svg).not.toBeNull();
    expect(svg!.querySelectorAll("path").length).toBeGreaterThan(0);
  });

  /*
   * A chart is an image to somebody who cannot see it.
   *
   * Without a role and a label, a screen reader either says nothing or reads
   * out the path data — which is worse, at length.
   */
  it("says what it is a picture of", () => {
    const element = anElement();
    svgCharts().draw(element, aSpec);

    const svg = element.querySelector("svg")!;
    expect(svg.getAttribute("role")).toBe("img");
    expect(svg.getAttribute("aria-label")).toBe("Messages sent each day this week");
  });

  /*
   * Bars start at zero unless somebody asked otherwise, because a bar chart
   * that starts at its smallest value exaggerates every difference in it —
   * the most common way a chart misleads with nobody intending it to.
   */
  it("includes zero by default", () => {
    const element = anElement();
    svgCharts().draw(element, {
      ...aSpec,
      kind: "bar",
      // All large and all similar: without zero these would look wildly
      // different from each other.
      series: [
        {
          name: "Messages",
          points: [
            { at: "Mon", value: 100 },
            { at: "Tue", value: 104 },
          ],
        },
      ],
    });

    const bars = [...element.querySelectorAll("rect")];
    const heights = bars.map((bar) => Number(bar.getAttribute("height")));

    // Within a few percent of each other, as the numbers are.
    expect(Math.abs(heights[0]! - heights[1]!) / heights[0]!).toBeLessThan(0.1);
  });

  it("exaggerates only when asked to", () => {
    const element = anElement();
    svgCharts().draw(element, {
      ...aSpec,
      kind: "bar",
      fromZero: false,
      series: [
        {
          name: "Messages",
          points: [
            { at: "Mon", value: 100 },
            { at: "Tue", value: 104 },
          ],
        },
      ],
    });

    const heights = [...element.querySelectorAll("rect")].map((bar) =>
      Number(bar.getAttribute("height")),
    );

    // Now the difference fills the chart, which is what not starting at zero
    // means and why it is not the default.
    expect(heights[0]).toBeLessThan(heights[1]! / 2);
  });

  /*
   * Everything a chart attached is removed with it.
   *
   * A resize observer left watching a removed element is the leak that shows
   * up as an application growing slower the longer somebody leaves it open.
   */
  it("removes what it added", () => {
    const disconnect = vi.fn();
    vi.stubGlobal(
      "ResizeObserver",
      class {
        observe() {}
        disconnect = disconnect;
      },
    );

    const element = anElement();
    const handle = svgCharts().draw(element, aSpec);

    expect(element.querySelector("svg")).not.toBeNull();

    handle.destroy();

    expect(element.querySelector("svg")).toBeNull();
    expect(disconnect).toHaveBeenCalled();
  });

  /*
   * An empty series draws nothing, rather than nonsense.
   *
   * Not throwing is not enough, and asserting only that was a test that
   * passed with the guard removed: Math.min of no values is Infinity, so the
   * scale silently becomes meaningless and the chart draws shapes at
   * coordinates nobody chose. Nothing is the honest answer to no data.
   */
  it("draws nothing when there is no data", () => {
    const element = anElement();

    expect(() =>
      svgCharts().draw(element, { ...aSpec, series: [{ name: "None", points: [] }] }),
    ).not.toThrow();

    const svg = element.querySelector("svg")!;
    expect(svg.querySelectorAll("path, rect")).toHaveLength(0);
    // And it still says what it would have shown, for anybody reading it.
    expect(svg.getAttribute("aria-label")).toBe(aSpec.describedAs);
  });
});
