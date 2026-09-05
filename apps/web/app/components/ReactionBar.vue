<script setup lang="ts">
import type { Reaction } from "@tdc/types";
import { SmilePlus } from "lucide-vue-next";
import { quickReactions } from "~/lib/emoji";

/**
 * The reactions on one message, and the way to add another.
 *
 * Counts rather than names, with the people revealed on hover: a message with
 * fifteen reactions should stay one line, and who reacted is a detail somebody
 * asks for rather than something that should crowd the conversation.
 */

defineProps<{
  reactions: Reaction[];
  /** Whether the reader may react at all — a non-member of a public room. */
  canReact?: boolean;
}>();

const emit = defineEmits<{ react: [emoji: string] }>();

const picking = ref(false);
const bar = ref<HTMLElement>();

/*
 * Closes the quick row when the reader clicks elsewhere.
 *
 * Without this it stays open while they scroll and read on, leaving a floating
 * strip of emoji over a conversation they have moved past.
 */
function onDocumentClick(event: MouseEvent) {
  if (!picking.value) return;
  if (bar.value?.contains(event.target as Node)) return;
  picking.value = false;
}

onMounted(() => document.addEventListener("click", onDocumentClick));
onBeforeUnmount(() => document.removeEventListener("click", onDocumentClick));

/** Who reacted, for the tooltip, with the reader named as themselves. */
function whoReacted(reaction: Reaction): string {
  const names = reaction.users.map((address) => address.split("@")[0] ?? address);

  if (names.length <= 6) return names.join(", ");
  return `${names.slice(0, 6).join(", ")} and ${names.length - 6} more`;
}
</script>

<template>
  <div v-if="reactions.length || canReact" ref="bar" class="flex flex-wrap items-center gap-1">
    <button
      v-for="reaction in reactions"
      :key="reaction.emoji"
      type="button"
      class="reaction"
      :data-mine="reaction.mine"
      :disabled="!canReact"
      :title="whoReacted(reaction)"
      @click="emit('react', reaction.emoji)"
    >
      <span class="emoji">{{ reaction.emoji }}</span>
      <span class="tabular-nums">{{ reaction.count }}</span>
    </button>

    <div v-if="canReact" class="relative">
      <button
        type="button"
        class="reaction reaction-add"
        aria-label="Add a reaction"
        title="Add a reaction"
        @click="picking = !picking"
      >
        <SmilePlus class="size-3.5" />
      </button>

      <!-- The six that carry most reactions, without opening the picker: the
           common case should be one click, not two.

           Opens downwards. Above would put it over the message being reacted
           to, which is exactly what the reader needs to see to choose. -->
      <div
        v-if="picking"
        class="absolute top-full left-0 z-20 mt-1 flex items-center gap-0.5 rounded-lg border border-border bg-card p-1 shadow-lg"
      >
        <button
          v-for="emoji in quickReactions"
          :key="emoji"
          type="button"
          class="quick-emoji"
          @click="
            emit('react', emoji);
            picking = false;
          "
        >
          {{ emoji }}
        </button>
      </div>
    </div>
  </div>
</template>

<style scoped>
@reference "~/assets/css/main.css";

.reaction {
  @apply flex cursor-pointer items-center gap-1 rounded-full border border-border bg-muted/50 px-1.5 py-0.5 text-xs transition-colors hover:bg-muted;
}

.reaction[data-mine="true"] {
  @apply border-primary/40 bg-primary/15 text-foreground;
}

.reaction:disabled {
  @apply cursor-default opacity-70 hover:bg-muted/50;
}

.reaction-add {
  @apply px-1.5 text-muted-foreground;
}

.emoji,
.quick-emoji {
  font-family:
    "Apple Color Emoji", "Segoe UI Emoji", "Noto Color Emoji", "Twemoji Mozilla", sans-serif;
}

.quick-emoji {
  @apply flex h-7 w-7 cursor-pointer items-center justify-center rounded-md text-base transition-colors hover:bg-accent;
}
</style>
