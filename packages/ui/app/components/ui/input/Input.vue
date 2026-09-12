<script setup lang="ts">
import type { HTMLAttributes, InputTypeHTMLAttribute } from "vue";
import { cn } from "#layers/ui/app/lib/utils";

/**
 * A single-line field.
 *
 * The same shape the .input class drew, behind a component so that the parts
 * easy to forget cannot be: what it is called, whether it is invalid, and which
 * message explains why. A field that is invalid says so to a screen reader
 * here, rather than relying on whoever wrote the page to remember aria-invalid.
 *
 * Colours are theme tokens, so each palette carries through untouched.
 */

const props = defineProps<{
  type?: InputTypeHTMLAttribute;
  placeholder?: string;
  disabled?: boolean;
  required?: boolean;
  autocomplete?: string;
  /** The field's name, for a screen reader where no visible label sits by it. */
  label?: string;
  /**
   * Takes focus when the screen opens.
   *
   * For the first field of a screen whose only purpose is that form — a sign-in
   * page, where anything else somebody might want to do is secondary to
   * typing. Not for a field inside a page that has other business, where
   * stealing focus moves somebody who was reading.
   */
  autofocus?: boolean;
  /** Whether what is in it was refused, and the message saying why. */
  invalid?: boolean;
  describedBy?: string;
  class?: HTMLAttributes["class"];
}>();

const model = defineModel<string>({ default: "" });
</script>

<template>
  <input
    v-model="model"
    :type="props.type ?? 'text'"
    :placeholder="props.placeholder"
    :disabled="props.disabled"
    :required="props.required"
    :autocomplete="props.autocomplete"
    :autofocus="props.autofocus"
    :aria-label="props.label"
    :aria-invalid="props.invalid || undefined"
    :aria-describedby="props.describedBy"
    :class="
      cn(
        'h-9 w-full rounded-md border border-border bg-surface px-2.5 text-sm text-foreground',
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
