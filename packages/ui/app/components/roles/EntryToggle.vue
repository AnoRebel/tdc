<script setup lang="ts">
import { Ban, Check, Minus } from "lucide-vue-next";

import type { EntryState } from "#layers/ui/app/lib/roles";

/**
 * What a role says about one permission: allow, deny, or nothing.
 *
 * Three native radio buttons drawn as one segmented control. Native, because a
 * radio group already does what this needs from a keyboard — one Tab stop, the
 * arrow keys to move, the choice announced with its group's name — and a
 * hand-built one would have to be taught each of those.
 */

const { $ts } = useI18n();

const props = defineProps<{
  modelValue: EntryState;
  /** The permission, named for somebody not seeing the row it sits in. */
  label: string;
  disabled?: boolean;
}>();

const emit = defineEmits<{ "update:modelValue": [state: EntryState] }>();

const name = useId();

const choices = computed(() => [
  {
    value: "grant" as const,
    text: $ts("roles.state.grant"),
    icon: Check,
    on: "bg-success/15 text-success-text",
  },
  {
    value: "inherit" as const,
    text: $ts("roles.state.inherit"),
    icon: Minus,
    on: "bg-muted text-foreground",
  },
  {
    value: "deny" as const,
    text: $ts("roles.state.deny"),
    icon: Ban,
    on: "bg-destructive/15 text-destructive",
  },
]);
</script>

<template>
  <fieldset
    class="inline-flex shrink-0 rounded-md border border-border bg-surface p-0.5"
    :disabled="props.disabled"
  >
    <legend class="sr-only">{{ props.label }}</legend>
    <label
      v-for="choice in choices"
      :key="choice.value"
      class="relative flex h-6 cursor-pointer items-center gap-1 rounded px-1.5 text-[0.6875rem] font-medium text-muted-foreground transition-colors duration-(--motion-duration-fast) ease-(--motion-ease-out) select-none has-focus-visible:ring-2 has-focus-visible:ring-ring has-disabled:cursor-not-allowed has-disabled:opacity-60"
      :class="props.modelValue === choice.value ? choice.on : 'hover:bg-accent'"
    >
      <input
        type="radio"
        class="sr-only"
        :name="name"
        :value="choice.value"
        :checked="props.modelValue === choice.value"
        :disabled="props.disabled"
        @change="emit('update:modelValue', choice.value)"
      />
      <component :is="choice.icon" class="size-3" aria-hidden="true" />
      <span>{{ choice.text }}</span>
    </label>
  </fieldset>
</template>
