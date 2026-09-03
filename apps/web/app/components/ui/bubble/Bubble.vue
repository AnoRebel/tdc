<script lang="ts" setup>
import type { PrimitiveProps } from "reka-ui";
import type { HTMLAttributes } from "vue";
import { Primitive } from "reka-ui";
import { cn } from "~/lib/utils";
import { bubbleVariants, type BubbleVariants } from ".";

interface Props extends PrimitiveProps {
  variant?: BubbleVariants["variant"];
  align?: "start" | "end";
  class?: HTMLAttributes["class"];
}

const props = withDefaults(defineProps<Props>(), {
  variant: "default",
  align: "start",
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
    :class="cn(bubbleVariants({ variant }), props.class)"
  >
    <slot />
  </Primitive>
</template>
