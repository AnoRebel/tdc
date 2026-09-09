<script lang="ts" setup>
import type { HTMLAttributes } from "vue";
import { cn } from "#layers/ui/app/lib/utils";

const props = defineProps<{ class?: HTMLAttributes["class"] }>();
</script>

<template>
  <kbd
    data-slot="kbd"
    :class="
      cn(
        'inline-flex h-5 items-center rounded border border-border bg-muted px-1.5 font-mono text-[0.6875rem] text-muted-foreground',
        props.class,
      )
    "
  >
    <slot />
  </kbd>
</template>
