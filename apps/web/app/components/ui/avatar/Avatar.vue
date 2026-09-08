<script lang="ts" setup>
import type { HTMLAttributes } from "vue";
import { cn } from "~/lib/utils";

/**
 * A person, shown as their initials on a colour derived from their address.
 *
 * Derived rather than stored: every account has one from the moment it exists,
 * with no upload step and no default image shared between people. The same
 * person is always the same colour, on every device and for every viewer,
 * because the address is the only input.
 *
 * Squircles rather than circles — circles are the generic choice, and the
 * softer square reads as a person without looking like every other product.
 */

const props = withDefaults(
  defineProps<{
    /** The person's address or name; the source of both initials and colour. */
    name: string;
    size?: "sm" | "md" | "lg";
    /** Draws a ring in the presence colour when the person is reachable. */
    online?: boolean;
    class?: HTMLAttributes["class"];
  }>(),
  { size: "md" },
);

/** The part before the "@", so an address does not become the initials. */
const local = computed(() => props.name.split("@")[0] ?? props.name);

const initials = computed(() => {
  const words = local.value.split(/[\s._-]+/).filter(Boolean);
  if (words.length >= 2) {
    return (words[0]![0]! + words[1]![0]!).toUpperCase();
  }
  return local.value.slice(0, 2).toUpperCase();
});

/**
 * A hue from the name, so the colour is stable without being stored.
 *
 * Lightness and chroma are fixed so every avatar sits at the same visual
 * weight; only the hue varies, which keeps a member list from looking like a
 * bag of sweets.
 */
const hue = computed(() => {
  let total = 0;
  for (const character of local.value) {
    total = (total * 31 + character.charCodeAt(0)) % 360;
  }
  return total;
});

const sizes = {
  sm: "size-6 text-[0.625rem] rounded-[0.4rem]",
  md: "size-8 text-xs rounded-[0.5rem]",
  lg: "size-10 text-sm rounded-[0.625rem]",
} as const;
</script>

<template>
  <span
    data-slot="avatar"
    :class="
      cn(
        'relative inline-grid shrink-0 place-items-center font-semibold',
        sizes[props.size],
        props.class,
      )
    "
    :style="{
      /*
       * Dark enough for white initials to be legible on it.
       *
       * At 0.62 some hues fall below the 4.5:1 that WCAG asks for, which axe
       * caught and an eye would not — the failing ones look fine until they
       * are the only thing telling two people apart. 0.52 clears it across
       * the whole wheel while keeping the colours distinct from each other.
       */
      background: `oklch(0.52 0.13 ${hue})`,
      color: 'oklch(0.99 0 0)',
    }"
    :title="props.name"
  >
    {{ initials }}
    <!-- A dot with a name, which needs a role to carry one: ARIA forbids
         labelling an element that has none, so a screen reader would either
         ignore this or announce a stray word with nothing attached to it. -->
    <span
      v-if="props.online"
      class="absolute -right-0.5 -bottom-0.5 size-2.5 rounded-full border-2 border-surface bg-success"
      role="img"
      aria-label="Online"
    />
  </span>
</template>
