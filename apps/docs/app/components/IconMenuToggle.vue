<script setup lang="ts">
/*
 * Replaces the layer's, which has no name: a screen reader announced it as
 * "button" and nothing more. Same bars-to-cross motion, done in CSS.
 */
defineProps<{ open: boolean }>();
</script>

<template>
  <UButton
    size="sm"
    variant="ghost"
    color="neutral"
    class="-me-1.5"
    square
    :aria-label="open ? 'Close menu' : 'Open menu'"
    :aria-expanded="open"
  >
    <svg
      class="size-5"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      stroke-width="2"
      stroke-linecap="round"
      aria-hidden="true"
    >
      <line x1="4" y1="6" x2="20" y2="6" class="bar" :class="{ 'top-open': open }" />
      <line x1="4" y1="12" x2="20" y2="12" class="bar" :class="{ 'middle-open': open }" />
      <line x1="4" y1="18" x2="20" y2="18" class="bar" :class="{ 'bottom-open': open }" />
    </svg>
  </UButton>
</template>

<style scoped>
.bar {
  transform-box: fill-box;
  transform-origin: center;
  transition:
    transform 180ms cubic-bezier(0.2, 0, 0, 1),
    opacity 120ms;
}
.top-open {
  transform: translateY(6px) rotate(45deg);
}
.middle-open {
  opacity: 0;
}
.bottom-open {
  transform: translateY(-6px) rotate(-45deg);
}
@media (prefers-reduced-motion: reduce) {
  .bar {
    transition: none;
  }
}
</style>
