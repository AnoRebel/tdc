<script setup lang="ts">
import { Bell } from "lucide-vue-next";
import { Button } from "#layers/ui/app/components/ui/button";
import { fully } from "#layers/ui/app/lib/dates";

/**
 * The web client's own settings: notifications while the application is closed.
 *
 * Here rather than on the shared settings page because they depend on a
 * service worker, which only the web client registers. This component replaces
 * the layer's empty one of the same name, so the shared page renders it in
 * place without knowing which client it is in.
 */

const push = usePushNotifications();

onMounted(() => void push.refresh());

/*
 * The checkbox writes through rather than binding both ways.
 *
 * A v-model would move the control the instant it is clicked and leave it
 * there whether or not the server agreed. Here it shows what the account says,
 * and only moves once the account says something different.
 */
async function chooseHidePushContent(event: Event) {
  const wanted = (event.target as HTMLInputElement).checked;

  try {
    await push.setHideContent(wanted);
  } catch {
    // Put back, because it never changed.
    (event.target as HTMLInputElement).checked = push.hideContent.value;
  }
}

function when(value: string): string {
  return fully(value);
}
</script>

<template>
  <!-- Only where the server offers it. A control that cannot work is
       worse than none: somebody presses it and concludes the application
       is broken rather than that the feature is not set up here. -->
  <section v-if="push.offered.value && push.supported.value">
    <h2 class="flex items-center gap-1.5 text-sm font-semibold">
      <Bell class="size-4" />
      {{ $t("notification.title") }}
    </h2>
    <p class="mt-1 text-xs text-muted-foreground">
      {{ $t("notification.whileClosed") }}
    </p>

    <p
      v-if="push.error.value"
      class="mt-2 rounded-md bg-destructive/10 px-3 py-2 text-xs text-destructive"
      role="alert"
    >
      {{ push.error.value }}
    </p>

    <Button
      class="mt-3"
      v-if="!push.subscribedHere.value"
      :disabled="push.busy.value"
      @click="push.subscribe()"
    >
      <Bell class="size-3.5" />
      {{ push.busy.value ? $t("notification.asking") : $t("notification.notifyThisDevice") }}
    </Button>

    <ul v-if="push.devices.value.length" class="mt-3 space-y-1.5">
      <li
        v-for="device in push.devices.value"
        :key="device.id"
        class="flex flex-wrap items-center justify-between gap-3 rounded-lg border bg-surface px-3 py-2.5"
      >
        <div class="min-w-0">
          <p class="truncate text-sm">
            {{ device.device }}
            <span
              v-if="device.current"
              class="ml-1.5 rounded bg-success/15 px-1.5 py-0.5 text-[0.6875rem] font-medium text-success-text"
            >
              {{ $t("auth.thisDevice") }}
            </span>
          </p>
          <p class="text-xs text-muted-foreground">
            <template v-if="device.lastUsedAt">
              {{ $t("notification.lastNotified", { when: when(device.lastUsedAt) }) }}
            </template>
            <template v-else>{{ $t("notification.notUsedYet") }}</template>
          </p>
        </div>
        <Button
          variant="ghost"
          class="h-7 px-2 text-xs"
          :disabled="push.busy.value"
          @click="push.unsubscribe(device.id)"
        >
          {{ $t("action.stop") }}
        </Button>
      </li>
    </ul>

    <!-- A payload is shown on a lock screen: to whoever holds the phone,
         and to anybody beside them. -->
    <label class="mt-3 flex cursor-pointer items-start gap-2">
      <input
        type="checkbox"
        class="mt-0.5 accent-primary"
        :checked="push.hideContent.value"
        :disabled="push.busy.value"
        @change="chooseHidePushContent"
      />
      <span class="min-w-0">
        <span class="block text-xs font-medium">{{ $t("notification.keepOffLockScreen") }}</span>
        <span class="block text-[0.6875rem] text-muted-foreground">
          {{ $t("notification.keepOffLockScreenHint") }}
        </span>
      </span>
    </label>
  </section>
</template>
