<script setup lang="ts">
import { SendHorizontal } from "lucide-vue-next";

import type { MessageControl } from "@tdc/types";
import { Button } from "#layers/ui/app/components/ui/button";
import { Input } from "#layers/ui/app/components/ui/input";
import { Spinner } from "#layers/ui/app/components/ui/spinner";

/**
 * A short field a message asks somebody to type into.
 *
 * One field and its send button, and nothing else: the only rule is "somebody
 * typed something", which the disabled button already states, and the server
 * checks the length it asked for regardless of what this allowed.
 *
 * Named by its visible label rather than by a placeholder, so what is being
 * asked is said before anybody starts typing and stays said once they have.
 */

const props = defineProps<{
  control: MessageControl;
  /** Unique on the page, tying the label to the field. */
  id: string;
  disabled?: boolean;
  /** Whether this field's answer is on its way. */
  sending?: boolean;
}>();

const emit = defineEmits<{ answer: [value: string] }>();

const { $ts } = useI18n();

const typed = ref("");

function submit() {
  const value = typed.value.trim();
  if (!value || props.disabled) return;
  emit("answer", value);
  typed.value = "";
}
</script>

<template>
  <form class="flex max-w-sm flex-col gap-1" @submit.prevent="submit">
    <label :for="props.id" class="text-xs font-medium">{{ props.control.label }}</label>
    <div class="flex items-center gap-1.5">
      <Input
        :id="props.id"
        v-model="typed"
        class="h-8"
        :placeholder="props.control.placeholder"
        :disabled="props.disabled"
        :maxlength="props.control.maxLength || 200"
      />
      <Button
        type="submit"
        variant="outline"
        size="icon"
        :disabled="props.disabled || !typed.trim()"
        :label="$ts('integrations.sendAnswer', { label: props.control.label })"
      >
        <Spinner v-if="props.sending" size="sm" />
        <SendHorizontal v-else class="size-3.5" aria-hidden="true" />
      </Button>
    </div>
  </form>
</template>
