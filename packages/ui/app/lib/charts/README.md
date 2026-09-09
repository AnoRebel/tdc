# Charts

Nothing in this application draws a chart yet. What exists is the interface one
would be drawn through, and a fallback that needs no dependency at all.

## Why an interface

The intended default is **TanStack Charts**, and it is alpha: thirty releases
in under a month, breaking changes between minors, and its own documentation
asking for exact version pins. It brings fourteen d3 packages and 2.7 MB
unpacked.

A library at that stage is one this application may have to leave. The
interface is what makes leaving a configuration change instead of a rewrite of
every screen that draws something — which is the design's stated reason for it,
not a precaution added afterwards.

## Adding TanStack Charts

Pin it exactly. Not `^0.16.2` — a caret on a 0.x version accepts the minor
releases that are precisely where this library breaks.

```jsonc
{
  "dependencies": {
    "@tanstack/vue-charts": "0.16.2", // exact, deliberately
  },
}
```

Then write an adapter beside `svg.ts` satisfying `ChartAdapter`, and provide it
in a plugin:

```ts
nuxtApp.vueApp.provide(chartAdapterKey, tanstackCharts());
```

Nothing else changes. `charts.test.ts` drives two implementations through one
sequence of calls, which is what "no caller changes" means and how it is
checked.

## The fallback

`svg.ts` draws lines, areas and bars in plain SVG with no dependency. It is not
a rival to a real charting library — it has no legends, no tooltips, no axes
worth the name — and that is the point. It is what remains when the alpha
dependency breaks, and it is always installable because it is already here.

It carries three rules a chart library would not necessarily give you:

- **`role="img"` and a label.** A chart is an image to somebody who cannot see
  it. Without this a screen reader either says nothing or reads out the path
  data, at length.
- **Zero on the axis by default.** A bar chart that starts at its smallest
  value exaggerates every difference in it, which is the most common way a
  chart misleads with nobody intending it to. `fromZero: false` is available
  and has to be asked for.
- **Everything it attaches is removed.** A resize observer left watching a
  removed element is the leak that shows up as an application growing slower
  the longer somebody leaves it open.
