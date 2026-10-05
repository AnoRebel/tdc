<script setup lang="ts">
import { Trash2 } from "lucide-vue-next";

import { Button } from "#layers/ui/app/components/ui/button";

/**
 * Removing an application, and deciding what happens to what it posted.
 *
 * Asked, not defaulted: keeping a bot's history and erasing it are both
 * legitimate, and the person removing it is the one who knows which this is.
 * Its credentials stop working either way, at once.
 */

const props = defineProps<{ name: string }>();
const emit = defineEmits<{ remove: [content: "retain" | "remove"]; cancel: [] }>();

const { $t, $ts } = useI18n();
const content = ref<"retain" | "remove" | null>(null);
</script>

<template>
  <!-- eslint-disable-next-line vuejs-accessibility/no-static-element-interactions -- a dialog's backdrop, closed by Escape or a click outside -->
  <div
    class="fixed inset-0 z-50 flex items-center justify-center bg-background/70 p-4 backdrop-blur-sm"
    role="dialog"
    aria-modal="true"
    :aria-label="$ts('integrations.removeWhom', { name: props.name })"
    @click.self="emit('cancel')"
    @keydown.esc="emit('cancel')"
  >
    <div class="w-[min(28rem,100%)] rounded-xl border border-border bg-card p-4 shadow-2xl">
      <h2 class="flex items-center gap-2 text-sm font-semibold">
        <Trash2 class="size-4" aria-hidden="true" />
        {{ $t("integrations.removeWhom", { name: props.name }) }}
      </h2>
      <p class="mt-1 text-xs text-muted-foreground">{{ $t("integrations.removeHint") }}</p>

      <form class="mt-4 space-y-3" @submit.prevent="content && emit('remove', content)">
        <fieldset>
          <legend class="mb-1.5 text-xs font-medium">
            {{ $t("integrations.whatItPosted") }}
          </legend>
          <label class="flex items-start gap-2 text-sm">
            <input v-model="content" type="radio" value="retain" class="mt-1 accent-primary" />
            <span>
              {{ $t("integrations.retainContent") }}
              <span class="block text-xs text-muted-foreground">
                {{ $t("integrations.retainContentHint") }}
              </span>
            </span>
          </label>
          <label class="mt-2 flex items-start gap-2 text-sm">
            <input v-model="content" type="radio" value="remove" class="mt-1 accent-primary" />
            <span>
              {{ $t("integrations.removeContent") }}
              <span class="block text-xs text-muted-foreground">
                {{ $t("integrations.removeContentHint") }}
              </span>
            </span>
          </label>
        </fieldset>

        <div class="flex justify-end gap-2">
          <Button variant="ghost" @click="emit('cancel')">{{ $t("action.cancel") }}</Button>
          <Button variant="danger" type="submit" :disabled="!content">
            {{ $t("integrations.removeApplication") }}
          </Button>
        </div>
      </form>
    </div>
  </div>
</template>
