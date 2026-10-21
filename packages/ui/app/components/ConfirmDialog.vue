<script setup lang="ts">
import { AlertTriangle } from "lucide-vue-next";
import { Button } from "#layers/ui/app/components/ui/button";

const { $t } = useI18n();

/**
 * Asks before something is destroyed.
 *
 * Only for what cannot be undone. A prompt on every action — including the
 * reversible ones — teaches people to dismiss prompts without reading them,
 * which is worse than no prompt at all, because then the one that mattered gets
 * dismissed too.
 *
 * The destructive button is not focused on open, and Enter does not activate
 * it. Someone reaching for a keyboard out of habit must not be able to destroy
 * something by reflex.
 */

const props = defineProps<{
  /** What is about to happen, e.g. "Delete this message?". */
  title: string;
  /** What will be lost. Shown verbatim, so the person recognises it. */
  detail?: string;
  /** The wording on the destructive button, e.g. "Delete". Defaults to that. */
  confirmLabel?: string;
  /** Defaults to "Cancel". */
  cancelLabel?: string;
}>();

const emit = defineEmits<{ confirm: []; cancel: [] }>();

// A component, not the element: the button inside it is what takes focus.
const cancelButton = ref<{ $el?: HTMLElement } | null>(null);

// Focus starts on cancel, which is both the safe default and what a plain
// Enter will therefore activate.
onMounted(async () => {
  await nextTick();
  cancelButton.value?.$el?.focus();
  document.body.style.overflow = "hidden";
});

onBeforeUnmount(() => {
  document.body.style.overflow = "";
});

// Escape cancels, as everywhere else.
onKeyStroke("Escape", () => emit("cancel"));
</script>

<template>
  <Teleport to="body">
    <div
      class="fixed inset-0 z-50 grid place-items-center bg-black/50 p-4 backdrop-blur-sm"
      role="dialog"
      aria-modal="true"
      :aria-label="props.title"
      @click.self="emit('cancel')"
    >
      <div
        class="animate-in fade-in zoom-in-95 w-full max-w-sm rounded-xl border bg-surface p-5 shadow-lg duration-150"
      >
        <div class="flex gap-3">
          <span class="grid size-9 shrink-0 place-items-center rounded-lg bg-destructive/12">
            <AlertTriangle class="size-4 text-destructive" />
          </span>

          <div class="min-w-0">
            <h2 class="text-sm font-semibold">{{ props.title }}</h2>
            <!-- Named rather than described: someone confirming a deletion
                 needs to see which thing they are about to lose. -->
            <p
              v-if="props.detail"
              class="mt-1 line-clamp-3 text-sm break-words text-muted-foreground"
            >
              {{ props.detail }}
            </p>
          </div>
        </div>

        <!-- Cancel first in the source, so it is what Tab reaches first and
             what a reflexive Enter activates. -->
        <div class="mt-5 flex justify-end gap-2">
          <Button variant="outline" ref="cancelButton" @click="emit('cancel')">
            {{ props.cancelLabel ?? $t("action.cancel") }}
          </Button>
          <Button
            class="bg-destructive text-destructive-foreground hover:opacity-90"
            @click="emit('confirm')"
          >
            {{ props.confirmLabel ?? $t("action.delete") }}
          </Button>
        </div>
      </div>
    </div>
  </Teleport>
</template>
