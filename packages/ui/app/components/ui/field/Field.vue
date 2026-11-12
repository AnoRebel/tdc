<script setup lang="ts">
import type { HTMLAttributes } from "vue";
import { useId } from "vue";

import { cn } from "#layers/ui/app/lib/utils";
import { Label } from "#layers/ui/app/components/ui/label";

/**
 * A field: its name, the control, and what is wrong with it.
 *
 * The three are wired together here rather than at each call site, because the
 * wiring is what gets forgotten. The label points at the control, the error
 * carries an id the control names in aria-describedby, and the error is
 * announced when it appears — so somebody who cannot see the red text is still
 * told why their entry was refused.
 *
 * The control is a slot and receives the id and the invalid state, so any of
 * the fields in this directory can sit inside one.
 */

const props = defineProps<{
  label: string;
  /** What is wrong, if anything is. Shown, announced, and pointed at. */
  error?: string;
  hint?: string;
  required?: boolean;
  class?: HTMLAttributes["class"];
}>();

const id = useId();
const errorId = `${id}-error`;
const hintId = `${id}-hint`;

const describedBy = computed(
  () =>
    [props.error ? errorId : null, props.hint ? hintId : null].filter(Boolean).join(" ") ||
    undefined,
);
</script>

<template>
  <div :class="cn('w-full', props.class)">
    <Label :for="id" :required="props.required">{{ props.label }}</Label>

    <slot :id="id" :invalid="Boolean(props.error)" :described-by="describedBy" />

    <p v-if="props.hint && !props.error" :id="hintId" class="mt-1 text-xs text-muted-foreground">
      {{ props.hint }}
    </p>

    <!-- Announced as it appears: somebody who cannot see it is told too. -->
    <p v-if="props.error" :id="errorId" role="alert" class="mt-1 text-xs text-destructive">
      {{ props.error }}
    </p>
  </div>
</template>
