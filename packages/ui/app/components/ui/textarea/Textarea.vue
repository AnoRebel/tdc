<script setup lang="ts">
import type { HTMLAttributes } from "vue";
import { cn } from "#layers/ui/app/lib/utils";

/** A field for several lines, drawn like the single-line one. */

const props = defineProps<{
  placeholder?: string;
  disabled?: boolean;
  required?: boolean;
  /*
   * Written as an attribute more often than bound, and an attribute is a
   * string — `rows="3"` is how anybody would write it, so both are taken
   * rather than making every call site remember a colon.
   */
  rows?: number | string;
  label?: string;
  invalid?: boolean;
  describedBy?: string;
  class?: HTMLAttributes["class"];
}>();

const model = defineModel<string>({ default: "" });
</script>

<template>
  <textarea
    v-model="model"
    :placeholder="props.placeholder"
    :disabled="props.disabled"
    :required="props.required"
    :rows="props.rows ?? 3"
    :aria-label="props.label"
    :aria-invalid="props.invalid || undefined"
    :aria-describedby="props.describedBy"
    :class="
      cn(
        'w-full rounded-md border border-border bg-surface px-2.5 py-2 text-sm text-foreground',
        'transition-[border-color,box-shadow] duration-150',
        'placeholder:text-muted-foreground',
        'focus-visible:border-ring focus-visible:ring-2 focus-visible:ring-ring/40 focus-visible:outline-none',
        'disabled:cursor-not-allowed disabled:opacity-50',
        props.invalid &&
          'border-destructive focus-visible:border-destructive focus-visible:ring-destructive/30',
        props.class,
      )
    "
  />
</template>
