<script setup lang="ts">
import type { HTMLAttributes } from "vue";
import { cn } from "~/lib/utils";

/**
 * An on/off switch.
 *
 * A real checkbox underneath rather than a styled div: it is focusable, it
 * responds to space, screen readers announce it, and forms and labels work —
 * all of which a div with a click handler has to reimplement, usually badly.
 * The visual switch is drawn from the checkbox's own state, so the two can
 * never disagree.
 */

const props = defineProps<{
  disabled?: boolean;
  /** Describes what the switch controls, for people not seeing the label. */
  label?: string;
  class?: HTMLAttributes["class"];
}>();

const model = defineModel<boolean>({ required: true });
</script>

<template>
  <label
    :class="
      cn(
        'relative inline-flex h-5 w-9 shrink-0 cursor-pointer items-center rounded-full transition-colors',
        'has-[:focus-visible]:ring-2 has-[:focus-visible]:ring-ring has-[:focus-visible]:ring-offset-2 has-[:focus-visible]:ring-offset-background',
        model ? 'bg-primary' : 'bg-muted-foreground/30',
        props.disabled && 'cursor-not-allowed opacity-50',
        props.class,
      )
    "
  >
    <input
      v-model="model"
      type="checkbox"
      role="switch"
      class="sr-only"
      :disabled="props.disabled"
      :aria-label="props.label"
    />
    <span
      :class="
        cn(
          'pointer-events-none block size-4 rounded-full bg-background shadow-sm transition-transform',
          model ? 'translate-x-4.5' : 'translate-x-0.5',
        )
      "
    />
  </label>
</template>
