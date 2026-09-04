<script lang="ts" setup>
import type { PrimitiveProps } from "reka-ui";
import type { HTMLAttributes } from "vue";
import { Primitive } from "reka-ui";
import { cn } from "~/lib/utils";
import { bubbleVariants, type BubbleVariants } from ".";

interface Props extends PrimitiveProps {
  variant?: BubbleVariants["variant"];
  align?: "start" | "end";
  /** True for every bubble after the first in a run from the same person. */
  continued?: boolean;
  class?: HTMLAttributes["class"];
}

const props = withDefaults(defineProps<Props>(), {
  variant: "default",
  align: "start",
  continued: false,
  as: "div",
});
</script>

<template>
  <Primitive
    data-slot="bubble"
    :data-variant="variant"
    :data-align="align"
    :as="as"
    :as-child="asChild"
    :class="cn(bubbleVariants({ variant, align, continued }), props.class)"
  >
    <slot />
  </Primitive>
</template>
