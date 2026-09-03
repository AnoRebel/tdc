<script lang="ts" setup>
import type { HTMLAttributes } from "vue";
import { cn } from "~/lib/utils";

const props = defineProps<{ class?: HTMLAttributes["class"] }>();
</script>

<template>
  <span
    data-slot="spinner"
    role="status"
    aria-label="Loading"
    :class="
      cn(
        'inline-block size-4 animate-spin rounded-full border-2 border-current border-t-transparent',
        props.class,
      )
    "
  />
</template>
