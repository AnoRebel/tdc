<script setup lang="ts">
import { RefreshCw, X } from "lucide-vue-next";
import { Button } from "#layers/ui/app/components/ui/button";

/**
 * Offers a new version, rather than applying one.
 *
 * A service worker that took over the moment it activated would reload the
 * page under whoever was typing. The waiting worker sits there until somebody
 * says yes, which is what `registerType: "prompt"` buys — and what makes it
 * worth asking rather than announcing.
 *
 * Drafts are saved as they are typed, so accepting cannot lose a half-written
 * message. That is what makes this prompt honest: it does not need to warn
 * about what it costs, because it costs nothing.
 */
const { $pwa } = useNuxtApp();
const { $t, $ts } = useI18n();

const dismissed = ref(false);
const updating = ref(false);

/*
 * Shown once the worker says there is something new and until somebody
 * answers. Dismissing hides it for this visit rather than for good: the new
 * version is still waiting, and the next load offers it again.
 */
const offering = computed(() => Boolean($pwa?.needRefresh) && !dismissed.value);

async function update() {
  updating.value = true;

  try {
    // Reloads the page, which is why nothing may live only in memory by now.
    await $pwa?.updateServiceWorker(true);
  } catch {
    // The old version keeps working, which is the point of not forcing this.
    updating.value = false;
    dismissed.value = true;
  }
}
</script>

<template>
  <Transition
    enter-active-class="transition duration-200 ease-out"
    enter-from-class="translate-y-2 opacity-0"
    leave-active-class="transition duration-150 ease-in"
    leave-to-class="translate-y-2 opacity-0"
  >
    <div
      v-if="offering"
      class="fixed inset-x-0 bottom-4 z-50 mx-auto flex w-fit max-w-[calc(100vw-2rem)] items-center gap-3 rounded-xl border bg-surface px-4 py-2.5 shadow-lg"
      role="status"
    >
      <RefreshCw class="size-4 shrink-0 text-primary" :class="updating && 'animate-spin'" />

      <p class="min-w-0 text-sm">
        {{ $t("update.ready") }}
        <span class="block text-xs text-muted-foreground">{{ $t("update.typedIsKept") }}</span>
      </p>

      <Button class="h-8" :disabled="updating" @click="update">
        {{ updating ? $t("update.updating") : $t("action.reload") }}
      </Button>

      <Button
        variant="ghost"
        class="size-8 shrink-0 p-0"
        :label="$ts('update.notNow')"
        @click="dismissed = true"
      >
        <X class="size-3.5" />
      </Button>
    </div>
  </Transition>
</template>
