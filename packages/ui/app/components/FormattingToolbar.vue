<script setup lang="ts">
import { Bold, CircleHelp, Code, Italic, SquareCode, Strikethrough } from "lucide-vue-next";

import type { Format } from "#layers/ui/app/lib/formatting";
import { commandKeyLabel } from "#layers/ui/app/lib/platform";
import { Button } from "#layers/ui/app/components/ui/button";

/**
 * The composer's formatting buttons, and a short reference for the syntax.
 *
 * Optional, and off until somebody asks for it: typing the markers and the
 * keyboard shortcuts cover the same ground, and a row of buttons costs a line
 * of the composer on a phone. Each button only asks the composer to wrap its
 * selection — the composer owns the text.
 *
 * The buttons keep focus in the message box (mousedown is prevented), so the
 * selection they act on is still there when they are pressed.
 */

const emit = defineEmits<{ format: [format: Format] }>();

const { $t, $ts } = useI18n();
// Ctrl or ⌘, as this platform writes it: a key name, never translated.
const command = commandKeyLabel();

const buttons = computed(() => [
  {
    format: "bold" as Format,
    icon: Bold,
    label: $ts("room.formatting.bold"),
    keys: `${command} B`,
  },
  {
    format: "italic" as Format,
    icon: Italic,
    label: $ts("room.formatting.italic"),
    keys: `${command} I`,
  },
  {
    format: "strikethrough" as Format,
    icon: Strikethrough,
    label: $ts("room.formatting.strikethrough"),
    keys: `${command} Shift X`,
  },
  {
    format: "code" as Format,
    icon: Code,
    label: $ts("room.formatting.code"),
    keys: `${command} E`,
  },
  {
    format: "codeBlock" as Format,
    icon: SquareCode,
    label: $ts("room.formatting.codeBlock"),
    keys: `${command} Shift C`,
  },
]);

/*
 * The syntax, as people type it. The examples are Markdown and stay as
 * written in every language; only what they are called is translated.
 */
const syntax = computed(() => [
  { example: "**bold**", name: $ts("room.formatting.bold") },
  { example: "_italic_", name: $ts("room.formatting.italic") },
  { example: "~~struck~~", name: $ts("room.formatting.strikethrough") },
  { example: "`code`", name: $ts("room.formatting.code") },
  { example: "```go\n…\n```", name: $ts("room.formatting.codeBlock") },
  { example: "> quoted", name: $ts("room.formatting.quote") },
  { example: "- item", name: $ts("room.formatting.list") },
  { example: "1. item", name: $ts("room.formatting.numberedList") },
  { example: "[text](https://…)", name: $ts("room.formatting.link") },
]);

const helping = ref(false);
const helpId = useId();
</script>

<template>
  <div
    class="relative mb-1 flex items-center gap-0.5"
    role="toolbar"
    :aria-label="$ts('room.formatting.toolbar')"
    data-composer-menu
  >
    <Button
      v-for="button in buttons"
      :key="button.format"
      variant="ghost"
      size="icon"
      class="size-7"
      :label="button.label"
      :title="`${button.label} (${button.keys})`"
      @mousedown.prevent
      @click="emit('format', button.format)"
    >
      <component :is="button.icon" class="size-3.5" aria-hidden="true" />
    </Button>

    <Button
      variant="ghost"
      size="icon"
      class="ml-auto size-7"
      :label="$ts('room.formatting.help')"
      :title="$ts('room.formatting.help')"
      :aria-expanded="helping"
      :aria-controls="helpId"
      @click="helping = !helping"
    >
      <CircleHelp class="size-3.5" aria-hidden="true" />
    </Button>

    <Transition name="motion-pop">
      <div
        v-if="helping"
        :id="helpId"
        class="absolute right-0 bottom-full z-20 mb-1 w-72 origin-bottom-right rounded-lg border border-border bg-surface-raised p-3 text-xs shadow-lg"
        role="region"
        :aria-label="$ts('room.formatting.help')"
      >
        <p class="text-muted-foreground">{{ $t("room.formatting.helpIntro") }}</p>
        <dl class="mt-2 grid grid-cols-[auto_1fr] gap-x-3 gap-y-1">
          <template v-for="row in syntax" :key="row.example">
            <dt>
              <code class="rounded bg-muted px-1 font-mono whitespace-pre">{{ row.example }}</code>
            </dt>
            <dd class="self-center">{{ row.name }}</dd>
          </template>
        </dl>
        <p class="mt-2 text-muted-foreground">{{ $t("room.formatting.enterInFence") }}</p>
      </div>
    </Transition>
  </div>
</template>
