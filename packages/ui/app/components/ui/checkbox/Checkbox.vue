<script setup lang="ts">
import type { HTMLAttributes } from "vue";
import { cn } from "#layers/ui/app/lib/utils";

/**
 * A tick box.
 *
 * A real checkbox underneath, for the reasons the switch gives: focus, space,
 * screen readers and label association all come free, and a styled div has to
 * reimplement every one of them.
 */

const props = defineProps<{
  id?: string;
  disabled?: boolean;
  label?: string;
  class?: HTMLAttributes["class"];
}>();

const model = defineModel<boolean>({ default: false });
</script>

<template>
  <input
    :id="props.id"
    v-model="model"
    type="checkbox"
    :disabled="props.disabled"
    :aria-label="props.label"
    :class="
      cn(
        'size-4 shrink-0 cursor-pointer rounded-sm border border-border accent-primary',
        'focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none',
        'disabled:cursor-not-allowed disabled:opacity-50',
        props.class,
      )
    "
  />
</template>
