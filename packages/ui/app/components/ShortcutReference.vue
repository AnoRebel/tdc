<script setup lang="ts">
import { commandKeyLabel } from "#layers/ui/app/lib/platform";
import { Keyboard } from "lucide-vue-next";
import { Kbd } from "#layers/ui/app/components/ui/kbd";
import { shortcuts } from "#layers/ui/app/composables/useShortcuts";

/**
 * What the keyboard does, for somebody who pressed "?".
 *
 * Built from the same list the shortcuts are bound from, so it cannot describe
 * a key that does nothing or omit one that works. A reference written by hand
 * beside the bindings is one that drifts, and a wrong reference is worse than
 * none — somebody trusts it and presses a key that does something else.
 */

const open = defineModel<boolean>({ default: false });

/** Grouped as the list declares, in the order the groups first appear. */
const groups = computed(() => {
  const byGroup = new Map<string, typeof shortcuts>();

  for (const shortcut of shortcuts) {
    const existing = byGroup.get(shortcut.group) ?? [];
    existing.push(shortcut);
    byGroup.set(shortcut.group, existing);
  }

  return [...byGroup.entries()].map(([name, items]) => ({ name, items }));
});

/*
 * The command key, written the way this platform writes it.
 *
 * Somebody on a Mac reading "Ctrl" tries Ctrl, which is a different modifier
 * there and does something else.
 */
const commandKey = computed(() => {
  if (import.meta.server) return "Ctrl";
  return commandKeyLabel();
});

function label(key: string): string {
  return key === "Ctrl" ? commandKey.value : key;
}
</script>

<template>
  <div
    v-if="open"
    class="fixed inset-0 z-50 flex items-center justify-center bg-background/70 p-4 backdrop-blur-sm"
    role="dialog"
    aria-modal="true"
    aria-label="Keyboard shortcuts"
    @click.self="open = false"
    @keydown.esc="open = false"
  >
    <div
      class="max-h-[min(32rem,90dvh)] w-[min(28rem,100%)] overflow-y-auto rounded-xl border border-border bg-card p-4 shadow-2xl"
    >
      <h2 class="flex items-center gap-2 text-sm font-semibold">
        <Keyboard class="size-4" />
        Keyboard shortcuts
      </h2>

      <div v-for="group in groups" :key="group.name" class="mt-4">
        <h3 class="text-xs font-medium text-muted-foreground">{{ group.name }}</h3>

        <dl class="mt-1.5 space-y-1">
          <div
            v-for="shortcut in group.items"
            :key="shortcut.description"
            class="flex items-center justify-between gap-4 rounded-md px-2 py-1.5 transition-colors hover:bg-accent"
          >
            <dt class="min-w-0 text-sm">{{ shortcut.description }}</dt>
            <dd class="flex shrink-0 items-center gap-1">
              <Kbd v-for="key in shortcut.keys" :key="key">{{ label(key) }}</Kbd>
            </dd>
          </div>
        </dl>
      </div>

      <div class="mt-4 flex justify-end">
        <button type="button" class="btn btn-ghost" @click="open = false">Close</button>
      </div>
    </div>
  </div>
</template>
