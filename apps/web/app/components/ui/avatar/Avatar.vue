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
      background: `oklch(0.62 0.13 ${hue})`,
      color: 'oklch(0.99 0 0)',
    }"
    :title="props.name"
  >
    {{ initials }}
    <span
      v-if="props.online"
      class="absolute -right-0.5 -bottom-0.5 size-2.5 rounded-full border-2 border-surface bg-success"
      aria-label="online"
    />
  </span>
</template>
