<script setup lang="ts">
import type { CallReactionKind } from "@tdc/types";
import { Hand, SmilePlus } from "lucide-vue-next";
import { Button } from "#layers/ui/app/components/ui/button";

/**
 * Raising a hand and sending a passing reaction — the things an audience does
 * during a talk without taking the floor.
 */

const { $t, $ts } = useI18n();
const c = useCall();
const open = ref(false);

const kinds: { kind: CallReactionKind; emoji: string }[] = [
  { kind: "clap", emoji: "👏" },
  { kind: "laugh", emoji: "😂" },
  { kind: "heart", emoji: "❤️" },
  { kind: "thumbs_up", emoji: "👍" },
  { kind: "surprise", emoji: "😮" },
  { kind: "celebrate", emoji: "🎉" },
];

function send(kind: CallReactionKind) {
  void c.react(kind);
}
</script>

<template>
  <div class="relative">
    <Button
      variant="ghost"
      size="icon"
      :label="$ts('calls.reactions.open')"
      :title="$ts('calls.reactions.open')"
      :aria-expanded="open"
      @click="open = !open"
    >
      <Hand v-if="c.handUp.value" class="size-4 text-warning" />
      <SmilePlus v-else class="size-4" />
    </Button>
    <div
      v-if="open"
      class="absolute bottom-full left-1/2 z-10 mb-2 flex -translate-x-1/2 items-center gap-0.5 rounded-full border bg-background p-1 shadow-lg"
      role="toolbar"
      :aria-label="$ts('calls.reactions.open')"
      @keydown.esc="open = false"
    >
      <Button
        variant="ghost"
        size="small"
        class="h-8 rounded-full px-2 text-xs"
        :aria-pressed="c.handUp.value"
        @click="c.setHand(!c.handUp.value)"
      >
        <Hand class="size-3.5" aria-hidden="true" />
        {{ c.handUp.value ? $t("calls.reactions.lower") : $t("calls.reactions.raise") }}
      </Button>
      <button
        v-for="each in kinds"
        :key="each.kind"
        type="button"
        class="grid size-8 place-items-center rounded-full text-lg transition-transform hover:scale-125 hover:bg-muted focus-visible:outline-2 focus-visible:outline-ring"
        :aria-label="$ts(`calls.reactions.${each.kind}`)"
        :title="$ts(`calls.reactions.${each.kind}`)"
        @click="send(each.kind)"
      >
        {{ each.emoji }}
      </button>
    </div>
  </div>
</template>
