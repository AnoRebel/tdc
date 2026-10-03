<script setup lang="ts">
import type { ChartHandle, ChartSpec } from "@tdc/types";

/**
 * A chart, drawn by whichever adapter this application provided.
 *
 * This is the whole of what a screen touches. Which library is underneath is
 * decided once, in a plugin, and swapping it changes nothing here and nothing
 * in any page — which is the point of the interface rather than a pleasant
 * side effect of it.
 */
const props = defineProps<{ spec: ChartSpec }>();

const charts = useCharts();
const host = useTemplateRef<HTMLElement>("host");

let handle: ChartHandle | undefined;

/*
 * Whether the chart could not be drawn.
 *
 * A library that throws — the alpha default breaking between releases, a
 * webview missing something it assumes — must not take the figures with it.
 * The page's `fallback` slot is shown instead: the same numbers, readable
 * without any drawing at all. A chart is a picture of data the caller already
 * has, so a caller can always say it in words.
 */
const failed = ref(false);

function attempt(draw: () => void) {
  try {
    draw();
  } catch (cause) {
    failed.value = true;
    handle?.destroy();
    handle = undefined;
    // Said once, where somebody debugging would look; the page carries on.
    console.warn("chart could not be drawn; showing its figures instead", cause);
  }
}

onMounted(() => {
  attempt(() => {
    if (host.value) handle = charts.draw(host.value, props.spec);
  });
});

// Redrawn rather than rebuilt, so whatever the library set up survives a data
// change — listeners, scales, and anything it caches.
watch(
  () => props.spec,
  (next) => {
    if (!failed.value) attempt(() => handle?.update(next));
  },
  { deep: true },
);

/*
 * Torn down when the component goes.
 *
 * A chart holds a resize observer, and one left watching a removed element is
 * the leak that shows as an application growing slower the longer it is open.
 */
onUnmounted(() => {
  handle?.destroy();
  handle = undefined;
});
</script>

<template>
  <!-- A size of its own, because an SVG asked to fill a parent with no height
       fills nothing and draws an invisible chart. -->
  <div v-show="!failed" ref="host" class="h-40 w-full" />
  <div v-if="failed" data-chart-fallback>
    <slot name="fallback" />
  </div>
</template>
