<script setup lang="ts">
import { ExternalLink } from "lucide-vue-next";

import type { MessageControl } from "@tdc/types";
import { renderableControlKinds } from "@tdc/types";
import { Button } from "#layers/ui/app/components/ui/button";
import { Select } from "#layers/ui/app/components/ui/select";
import { Spinner } from "#layers/ui/app/components/ui/spinner";

/**
 * The controls a message offers: buttons, menus, and short fields.
 *
 * Real controls, not styled divs: a reader using a keyboard reaches them by
 * tabbing, activates them with Enter or Space, and hears "button", "combo box"
 * or "edit text" — all of which come free from the element and none of which
 * come from a div with a click handler. A menu opens a list of its choices; a
 * field is named by its visible label, so what is being asked is said before
 * anybody starts typing.
 *
 * The outcome is announced rather than only shown. Somebody who cannot see the
 * row change needs to be told the press was accepted, and told it once —
 * which is why the live region carries the result rather than the row being
 * marked live and re-announcing every control each time one changes.
 */

const props = defineProps<{
  actions: MessageControl[];
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
  /** Distinguishes this message's fields from another's with the same names. */
  messageId?: string;
}>();

const emit = defineEmits<{ press: [action: string, value?: string] }>();

/** Which control is waiting, so it can say so and not be pressed twice. */
const pressing = ref<string | null>(null);

/** What has been picked from each menu, by name. */
const picked = ref<Record<string, string | undefined>>({});

const { $t, $ts } = useI18n();

/**
 * The controls this client knows how to show.
 *
 * A kind it does not recognise is left out rather than drawn as something it
 * is not — a future control drawn as a button would send a press the program
 * never meant to be possible. The message's text is shown regardless.
 */
const shown = computed(() =>
  props.actions.filter((action) => renderableControlKinds.has(action.kind)),
);
const buttons = computed(() => shown.value.filter((action) => !action.kind));
const fields = computed(() => shown.value.filter((action) => action.kind));

/**
 * Whether a control can still be used.
 *
 * An expired control is left visible and disabled rather than removed: one
 * that vanishes leaves somebody wondering whether they imagined it, and a
 * reader who tabbed to it would lose their place.
 */
function expired(action: MessageControl): boolean {
  return action.expires !== undefined && new Date(action.expires) <= new Date();
}

function unavailable(action: MessageControl): boolean {
  return !props.canPress || expired(action) || pressing.value !== null;
}

/** An identifier unique to this control on this message, for label wiring. */
function fieldId(action: MessageControl): string {
  return `control-${props.messageId ?? "message"}-${action.name}`.replace(/[^a-zA-Z0-9_-]/g, "");
}

async function activate(action: MessageControl, value?: string) {
  if (unavailable(action)) return;

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
    emit("press", action.name, value);
  } finally {
    pressing.value = null;
  }
}

/** A menu answers as soon as something is chosen: choosing is the answer. */
function choose(action: MessageControl, value: string | undefined) {
  picked.value = { ...picked.value, [action.name]: value };
  if (value) void activate(action, value);
}
</script>

<template>
  <div v-if="shown.length" class="mt-2 flex flex-col gap-2">
    <div v-if="buttons.length" class="flex flex-wrap items-center gap-1.5">
      <Button
        v-for="action in buttons"
        :key="action.name"
        type="button"
        variant="outline"
        size="small"
        :disabled="unavailable(action)"
        :label="
          expired(action)
            ? $ts('room.actionNoLongerOffered', { label: action.label })
            : action.label
        "
        @click="activate(action)"
      >
        <Spinner v-if="pressing === action.name" size="sm" />
        <ExternalLink
          v-else-if="action.capability === 'open'"
          class="size-3.5"
          aria-hidden="true"
        />
        {{ action.label }}
      </Button>
    </div>

    <template v-for="action in fields" :key="action.name">
      <!-- A menu: the label names the question, and the list is its answers. -->
      <div v-if="action.kind === 'select'" class="flex max-w-xs flex-col gap-1">
        <span class="text-xs font-medium" aria-hidden="true">{{ action.label }}</span>
        <Select
          :model-value="picked[action.name]"
          :options="(action.options ?? []).map((option) => ({ ...option }))"
          :placeholder="$ts('integrations.chooseOne')"
          :label="action.label"
          :disabled="unavailable(action)"
          class="h-8"
          @update:model-value="(value) => choose(action, value as string | undefined)"
        />
      </div>

      <!-- A field: named by its visible label, sent with Enter or the button. -->
      <MessageControlField
        v-else-if="action.kind === 'input'"
        :id="fieldId(action)"
        :control="action"
        :disabled="unavailable(action)"
        :sending="pressing === action.name"
        @answer="(value: string) => activate(action, value)"
      />
    </template>

    <!-- Said once, when there is something to say. Somebody who cannot see the
         row change is told the press was accepted; polite, because it is not
         worth interrupting whatever they were reading. -->
    <p v-if="props.outcome" class="sr-only" role="status" aria-live="polite">
      {{ props.outcome }}
    </p>

    <!-- Said in the row rather than only by disabling: a disabled control
         announces "unavailable" without saying why, which leaves somebody
         guessing whether it is them or the control. -->
    <p v-if="!props.canPress" class="text-[0.6875rem] text-muted-foreground">
      {{ $t("room.joinToAnswer") }}
    </p>
  </div>
</template>
