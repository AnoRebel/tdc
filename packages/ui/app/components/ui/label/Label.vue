<script setup lang="ts">
import type { HTMLAttributes } from "vue";
import { cn } from "#layers/ui/app/lib/utils";

/**
 * The name of a field, tied to it.
 *
 * `for` is required rather than optional: a label that is not tied to anything
 * is decoration, and clicking it does nothing — which is the small failure
 * nobody notices until they are trying to hit a checkbox on a phone.
 */

const props = defineProps<{
  for: string;
  required?: boolean;
  class?: HTMLAttributes["class"];
}>();
</script>

<template>
  <label
    :for="props.for"
    :class="cn('mb-1.5 block text-sm font-medium text-foreground', props.class)"
  >
    <slot />
    <span v-if="props.required" class="text-destructive" aria-hidden="true"> *</span>
  </label>
</template>
