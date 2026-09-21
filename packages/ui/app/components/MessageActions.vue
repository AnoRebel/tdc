<script setup lang="ts">
import { ExternalLink } from "lucide-vue-next";

import type { MessageAction } from "@tdc/types";
import { Button } from "#layers/ui/app/components/ui/button";
import { Spinner } from "#layers/ui/app/components/ui/spinner";

/**
 * The buttons a message offers.
 *
 * Real buttons, not styled divs: a reader using a keyboard reaches them by
 * tabbing, activates them with Enter or Space, and hears "button" — all of
 * which come free from the element and none of which come from a div with a
 * click handler.
 *
 * The outcome is announced rather than only shown. Somebody who cannot see the
 * row change needs to be told the press was accepted, and told it once —
 * which is why the live region carries the result rather than the row being
 * marked live and re-announcing every button each time one changes.
 */

const props = defineProps<{
  actions: MessageAction[];
  /** Absent while the reader is not a member, which is when nothing is
   * pressable: answering a question in a channel is taking part in it. */
  canPress?: boolean;
  /**
   * What to say about the last press, for the live region.
   *
   * Given by the parent rather than held here, because the outcome arrives as
   * an event over the live connection rather than as the answer to the press
   * — so what a reader is told is what actually happened.
   */
  outcome?: string;
}>();

const emit = defineEmits<{ press: [action: string] }>();

/** Which button is waiting, so it can say so and not be pressed twice. */
const pressing = ref<string | null>(null);

const { $t, $ts } = useI18n();

/**
 * Whether a button can still be pressed.
 *
 * An expired action is left visible and disabled rather than removed: a button
 * that vanishes leaves somebody wondering whether they imagined it, and a
 * reader who tabbed to it would lose their place.
 */
function expired(action: MessageAction): boolean {
  return action.expires !== undefined && new Date(action.expires) <= new Date();
}

async function activate(action: MessageAction) {
  if (expired(action) || pressing.value) return;

  /*
   * A capability the client performs itself. Opening happens here and nothing
   * is sent, because the press was never a question for the program — the
   * whole reason `open` exists rather than a round trip.
   */
  if (action.capability === "open") {
    if (action.url) window.open(action.url, "_blank", "noopener,noreferrer");
    return;
  }

  pressing.value = action.name;
  try {
    emit("press", action.name);
  } finally {
    pressing.value = null;
  }
}
</script>

<template>
  <div v-if="props.actions.length" class="mt-2 flex flex-wrap items-center gap-1.5">
    <Button
      v-for="action in props.actions"
      :key="action.name"
      type="button"
      variant="outline"
      size="small"
      :disabled="!props.canPress || expired(action) || pressing !== null"
      :label="
        expired(action) ? $ts('room.actionNoLongerOffered', { label: action.label }) : action.label
      "
      @click="activate(action)"
    >
      <Spinner v-if="pressing === action.name" size="sm" />
      <ExternalLink v-else-if="action.capability === 'open'" class="size-3.5" aria-hidden="true" />
      {{ action.label }}
    </Button>

    <!-- Said once, when there is something to say. Somebody who cannot see the
         row change is told the press was accepted; polite, because it is not
         worth interrupting whatever they were reading. -->
    <p v-if="props.outcome" class="sr-only" role="status" aria-live="polite">
      {{ props.outcome }}
    </p>

    <!-- Said in the row rather than only by disabling: a disabled button
         announces "unavailable" without saying why, which leaves somebody
         guessing whether it is them or the button. -->
    <p v-if="!props.canPress" class="text-[0.6875rem] text-muted-foreground">
      {{ $t("room.joinToAnswer") }}
    </p>
  </div>
</template>
