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

onMounted(() => {
  if (host.value) handle = charts.draw(host.value, props.spec);
});

// Redrawn rather than rebuilt, so whatever the library set up survives a data
// change — listeners, scales, and anything it caches.
watch(
  () => props.spec,
  (next) => handle?.update(next),
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
  <div ref="host" class="h-40 w-full" />
</template>
