<script lang="ts" setup>
import type { HTMLAttributes } from "vue";
import { cn } from "~/lib/utils";

/**
 * An indeterminate wait.
 *
 * Only for waits with no shape to predict — a message in flight, a
 * reconnection. Where the shape of what is coming is known, a Skeleton says
 * more, because it shows the layout instead of only that something is
 * happening.
 *
 * The ring is drawn from the current text colour at partial opacity with one
 * solid arc, rather than a hard border-plus-transparent, so it reads as a
 * sweep rather than a chasing dot.
 */

const props = withDefaults(
  defineProps<{
    /** Matches the type sizes it sits beside. */
    size?: "sm" | "md" | "lg";
    class?: HTMLAttributes["class"];
  }>(),
  { size: "md" },
);

const sizes = {
  sm: "size-3 border-[1.5px]",
  md: "size-4 border-2",
  lg: "size-6 border-2",
} as const;
</script>

<template>
  <span
    data-slot="spinner"
    role="status"
    aria-label="Loading"
    :class="
      cn(
        'inline-block shrink-0 animate-spin rounded-full',
        'border-current/25 border-t-current',
        sizes[props.size],
        props.class,
      )
    "
  />
</template>
