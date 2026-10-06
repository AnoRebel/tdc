<script setup lang="ts">
import type { HTMLAttributes } from "vue";
import { cn } from "#layers/ui/app/lib/utils";

/**
 * A button, in the shapes this application actually uses.
 *
 * The variants are the .btn classes the stylesheet already defined, moved
 * behind a component for one reason that is not tidiness: an icon-only button
 * has to say what it does. The markup this replaces carried `title="Edit"` and
 * nothing else, which shows a tooltip to a pointer and says nothing to a screen
 * reader. Here the label is a prop, and an icon-only button without one does
 * not type-check.
 *
 * Every colour is a theme token rather than a literal, so a palette change
 * carries through without touching this file — which is the whole point of
 * having tokens.
 */

type Variant = "primary" | "ghost" | "outline" | "danger";
type Size = "default" | "icon" | "small";

const props = withDefaults(
  defineProps<{
    variant?: Variant;
    size?: Size;
    type?: "button" | "submit" | "reset";
    disabled?: boolean;
    /**
     * What this button does, for somebody not seeing it.
     *
     * Required when there is no text — an icon is not a name. Given as the
     * accessible name and as the tooltip, so the pointer and the screen reader
     * are told the same thing rather than only one of them.
     */
    label?: string;
    class?: HTMLAttributes["class"];
  }>(),
  { variant: "primary", size: "default", type: "button" },
);

const variants: Record<Variant, string> = {
  primary: "bg-primary text-primary-foreground shadow-xs hover:not-disabled:bg-primary-hover",
  ghost:
    "text-muted-foreground hover:not-disabled:bg-accent hover:not-disabled:text-accent-foreground",
  outline:
    "border border-border bg-surface text-foreground hover:not-disabled:bg-accent hover:not-disabled:border-border-strong",
  danger: "text-destructive hover:not-disabled:bg-destructive/10",
};

const sizes: Record<Size, string> = {
  default: "h-8 px-2.5",
  small: "h-6 px-1.5 text-[0.75rem]",
  icon: "size-8 px-0",
};
</script>

<template>
  <button
    :type="props.type"
    :disabled="props.disabled"
    :aria-label="props.label ?? undefined"
    :title="props.label ?? undefined"
    :class="
      cn(
        'inline-flex items-center justify-center gap-1.5 rounded-md text-[0.8125rem] font-medium whitespace-nowrap',
        'transition-[background-color,border-color,transform,opacity] duration-(--motion-duration-fast) ease-(--motion-ease-out)',
        // A press that moves is what makes a control feel physical.
        'active:not-disabled:translate-y-px',
        'focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none',
        'disabled:cursor-not-allowed disabled:opacity-50',
        variants[props.variant],
        sizes[props.size],
        props.class,
      )
    "
  >
    <slot />
  </button>
</template>
