<script setup lang="ts">
import { onKeyStroke } from "@vueuse/core";
import { Keyboard, SlidersHorizontal } from "lucide-vue-next";

import { commandKeyLabel, isMac } from "#layers/ui/app/lib/platform";
import { SHORTCUTS, SHORTCUT_GROUPS, bindingKeys } from "#layers/ui/app/lib/shortcuts";
import { Kbd } from "#layers/ui/app/components/ui/kbd";
import { Button } from "#layers/ui/app/components/ui/button";
import { Switch } from "#layers/ui/app/components/ui/switch";

/**
 * What the keyboard does, for somebody who pressed "?".
 *
 * Built from the same list the shortcuts are bound from, so it cannot describe
 * a key that does nothing or omit one that works. A reference written by hand
 * beside the bindings is one that drifts, and a wrong reference is worse than
 * none — somebody trusts it and presses a key that does something else.
 *
 * The switch for shortcuts lives here too, because this is where somebody who
 * keeps setting them off by accident will look for it.
 */

const open = defineModel<boolean>({ default: false });
const emit = defineEmits<{ preferences: [] }>();

const { $t, $ts } = useI18n();
const { shortcuts: enabled } = useInterfacePreferences();

const groups = computed(() =>
  SHORTCUT_GROUPS.map((group) => ({
    group,
    items: SHORTCUTS.filter((shortcut) => shortcut.group === group).map((shortcut) => ({
      id: shortcut.id,
      alwaysOn: Boolean(shortcut.alwaysOn),
      bindings: shortcut.bindings.map(bindingKeys),
    })),
  })),
);

/*
 * The modifiers, written the way this platform writes them.
 *
 * Somebody on a Mac reading "Ctrl" tries Ctrl, which is a different modifier
 * there and does something else.
 */
function label(key: string): string {
  if (key === "Ctrl") return commandKeyLabel();
  if (key === "Alt" && isMac()) return "⌥";
  if (key === "Shift" && isMac()) return "⇧";
  return key;
}

/*
 * Focus goes into the dialog when it appears, and back to where it came from
 * when it goes.
 *
 * Watched on the element rather than timed after a tick: the dialog is loaded
 * on first use and drawn inside a transition, so "after the next tick" was not
 * reliably "after it exists" — and a dialog that never takes focus leaves a
 * keyboard user typing into the page behind it.
 *
 * Not animated: a focus jump is where somebody's eyes and a screen reader's
 * cursor both are, and moving it smoothly would only delay it.
 */
const panel = useTemplateRef<HTMLElement>("panel");
let returnTo: HTMLElement | null = null;

watch(panel, (element) => {
  if (element) {
    returnTo = document.activeElement as HTMLElement | null;
    element.focus();
  } else {
    returnTo?.focus?.();
    returnTo = null;
  }
});

onKeyStroke("Escape", (event) => {
  if (!open.value) return;
  event.preventDefault();
  open.value = false;
});

function morePreferences() {
  open.value = false;
  emit("preferences");
}
</script>

<template>
  <Transition name="motion-fade">
    <!-- A modal backdrop: dismissed by clicking outside it or pressing
         Escape, which is handled for the whole window above. -->
    <!-- eslint-disable-next-line vuejs-accessibility/no-static-element-interactions -->
    <div
      v-if="open"
      class="fixed inset-0 z-50 flex items-center justify-center bg-background/70 p-4 backdrop-blur-sm"
      @click.self="open = false"
    >
      <Transition name="motion-pop" appear>
        <div
          class="max-h-[min(36rem,90dvh)] w-[min(32rem,100%)] overflow-y-auto rounded-xl border border-border bg-card p-4 shadow-2xl outline-none"
          ref="panel"
          tabindex="-1"
          role="dialog"
          aria-modal="true"
          aria-labelledby="shortcut-reference-title"
        >
          <h2 id="shortcut-reference-title" class="flex items-center gap-2 text-sm font-semibold">
            <Keyboard class="size-4" aria-hidden="true" />
            {{ $t("nav.keyboardShortcuts") }}
          </h2>

          <p
            v-if="!enabled"
            class="mt-2 rounded-md bg-muted px-2.5 py-2 text-xs text-muted-foreground"
          >
            {{ $t("interface.shortcutsOffNotice") }}
          </p>

          <section v-for="{ group, items } in groups" :key="group" class="mt-4">
            <h3 class="text-xs font-medium text-muted-foreground">
              {{ $t(`interface.shortcutGroups.${group}`) }}
            </h3>

            <dl class="mt-1.5 space-y-0.5">
              <div
                v-for="item in items"
                :key="item.id"
                class="motion-row flex items-center justify-between gap-4 rounded-md px-2 py-1.5 hover:bg-accent"
                :class="!enabled && !item.alwaysOn && 'opacity-60'"
              >
                <dt class="min-w-0 text-sm">{{ $t(`interface.shortcuts.${item.id}`) }}</dt>
                <dd class="flex shrink-0 flex-wrap items-center justify-end gap-1">
                  <template v-for="(binding, index) in item.bindings" :key="index">
                    <span v-if="index > 0" class="px-0.5 text-[0.6875rem] text-muted-foreground">
                      {{ $t("interface.or") }}
                    </span>
                    <template v-for="(key, position) in binding.keys" :key="position">
                      <span
                        v-if="binding.sequence && position > 0"
                        class="text-[0.6875rem] text-muted-foreground"
                      >
                        {{ $t("interface.then") }}
                      </span>
                      <Kbd>{{ label(key) }}</Kbd>
                    </template>
                  </template>
                </dd>
              </div>
            </dl>
          </section>

          <div class="mt-4 flex flex-wrap items-center gap-3 border-t pt-3">
            <Switch v-model="enabled" :label="$ts('interface.shortcutsLabel')" />
            <span class="text-xs">{{ $t("interface.shortcutsLabel") }}</span>
            <span class="ml-auto flex gap-1.5">
              <Button variant="ghost" @click="morePreferences">
                <SlidersHorizontal class="size-3.5" aria-hidden="true" />
                {{ $t("interface.more") }}
              </Button>
              <Button variant="outline" @click="open = false">
                {{ $t("action.close") }}
              </Button>
            </span>
          </div>
        </div>
      </Transition>
    </div>
  </Transition>
</template>
