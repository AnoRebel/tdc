<script setup lang="ts">
import { onKeyStroke } from "@vueuse/core";

import { Button } from "#layers/ui/app/components/ui/button";

/**
 * The interface preferences, from wherever somebody is.
 *
 * Reached from the command palette and the shortcut list, so changing motion
 * or switching shortcuts off never needs a trip to settings — the desktop
 * client has no settings page to go to at all.
 */

const open = defineModel<boolean>({ default: false });
const { $t, $ts } = useI18n();

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
</script>

<template>
  <Transition name="motion-fade">
    <!-- A modal backdrop, dismissed by clicking outside or pressing Escape. -->
    <!-- eslint-disable-next-line vuejs-accessibility/no-static-element-interactions -->
    <div
      v-if="open"
      class="fixed inset-0 z-50 flex items-center justify-center bg-background/70 p-4 backdrop-blur-sm"
      @click.self="open = false"
    >
      <Transition name="motion-pop" appear>
        <div
          class="max-h-[90dvh] w-[min(30rem,100%)] overflow-y-auto rounded-xl border border-border bg-card p-4 shadow-2xl outline-none"
          ref="panel"
          tabindex="-1"
          role="dialog"
          aria-modal="true"
          :aria-label="$ts('interface.title')"
        >
          <InterfacePreferences heading />
          <div class="mt-5 flex justify-end">
            <Button variant="outline" @click="open = false">
              {{ $t("action.close") }}
            </Button>
          </div>
        </div>
      </Transition>
    </div>
  </Transition>
</template>
