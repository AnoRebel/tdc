<script setup lang="ts" generic="T extends string | number">
import { Check, ChevronDown } from "lucide-vue-next";
import {
  SelectContent,
  SelectIcon,
  SelectItem,
  SelectItemIndicator,
  SelectItemText,
  SelectPortal,
  SelectRoot,
  SelectTrigger,
  SelectValue,
  SelectViewport,
} from "reka-ui";

import { cn } from "#layers/ui/app/lib/utils";

/**
 * A choice from a list.
 *
 * Not a native <select>, and the reason is this application's second client: a
 * native one is drawn by the platform, so the same screen looks like GTK in the
 * desktop webview and like Chrome in a browser, with different metrics, a
 * different open behaviour and a different idea of what a focused option looks
 * like. Drawn here, both clients show the same control — which is the whole
 * claim the shared layer makes.
 *
 * The keyboard, the typeahead and the ARIA come from reka-ui underneath, so
 * this file is the appearance and nothing else: every colour a theme token,
 * which is how a palette reaches it without this being touched.
 */

const props = defineProps<{
  options: { value: T; label: string; disabled?: boolean }[];
  placeholder?: string;
  disabled?: boolean;
  /** What is being chosen, for somebody not seeing the label beside it. */
  label?: string;
  class?: string;
}>();

const model = defineModel<T>();
</script>

<template>
  <SelectRoot v-model="model" :disabled="props.disabled">
    <SelectTrigger
      :aria-label="props.label"
      :class="
        cn(
          'flex h-9 w-full items-center justify-between gap-2 rounded-md border border-border bg-surface px-2.5',
          'text-sm text-foreground transition-[border-color,box-shadow] duration-150',
          'focus-visible:border-ring focus-visible:ring-2 focus-visible:ring-ring/40 focus-visible:outline-none',
          'disabled:cursor-not-allowed disabled:opacity-50',
          'data-[placeholder]:text-muted-foreground',
          props.class,
        )
      "
    >
      <SelectValue :placeholder="props.placeholder ?? 'Choose'" />
      <SelectIcon as-child>
        <ChevronDown class="size-4 shrink-0 text-muted-foreground" />
      </SelectIcon>
    </SelectTrigger>

    <SelectPortal>
      <SelectContent
        position="popper"
        :side-offset="4"
        class="z-50 max-h-64 min-w-(--reka-select-trigger-width) overflow-hidden rounded-md border border-border bg-surface-raised shadow-lg"
      >
        <SelectViewport class="p-1">
          <SelectItem
            v-for="option in props.options"
            :key="String(option.value)"
            :value="option.value"
            :disabled="option.disabled"
            class="relative flex cursor-pointer items-center gap-2 rounded-sm py-1.5 pr-2 pl-7 text-sm text-foreground select-none data-highlighted:bg-accent data-highlighted:text-accent-foreground data-highlighted:outline-none data-[disabled]:pointer-events-none data-[disabled]:opacity-50"
          >
            <SelectItemIndicator class="absolute left-2 inline-flex items-center">
              <Check class="size-3.5" />
            </SelectItemIndicator>
            <SelectItemText>{{ option.label }}</SelectItemText>
          </SelectItem>
        </SelectViewport>
      </SelectContent>
    </SelectPortal>
  </SelectRoot>
</template>
