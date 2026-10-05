<script setup lang="ts">
import { Bell, ChevronRight, Download, Laptop, PanelsTopLeft, Server } from "lucide-vue-next";
import { Button } from "#layers/ui/app/components/ui/button";

/**
 * The desktop client's own settings, on the shared settings page.
 *
 * What belongs to this installation rather than to the account: which server it
 * talks to, whether the system may show notifications, the window, and updates.
 * This replaces the layer's empty component of the same name, so the shared
 * page renders it in place — the web client puts its service-worker
 * notifications in the same spot.
 *
 * The window and updates keep their own screens, which the menu opens
 * directly; this links to them rather than repeating them, so there is one
 * place each setting lives.
 */

const { $t, $localeRoute: localeRoute } = useI18n();

const { servers, refresh: refreshServers } = useServers();
const notifications = useDesktopNotifications();

const current = computed(() => servers.value.find((server) => server.current)?.url ?? "");

/*
 * Whether asking was refused, kept apart from whether notifications are on.
 *
 * Some platforms ask only once, so after a refusal the button would do
 * nothing; saying where the setting now lives is the only useful answer.
 */
const refused = ref(false);

async function turnOnNotifications() {
  refused.value = !(await notifications.ask());
}

onMounted(() => {
  // Neither is worth an error on a settings page: an unknown server shows no
  // address, and an unanswerable platform shows notifications as off.
  void refreshServers().catch(() => {});
  void notifications.refresh().catch(() => {});
});

const screens = computed(() => [
  {
    name: "appearance" as const,
    label: $t("desktop.settings.windowAndStartup"),
    icon: PanelsTopLeft,
  },
  { name: "updates" as const, label: $t("desktop.settings.updates"), icon: Download },
]);
</script>

<template>
  <section>
    <h2 class="flex items-center gap-1.5 text-sm font-semibold">
      <Laptop class="size-4" />
      {{ $t("desktop.settings.title") }}
    </h2>
    <p class="mt-1 text-xs text-muted-foreground">{{ $t("desktop.settings.hint") }}</p>

    <ul class="mt-3 space-y-1.5">
      <li
        class="flex flex-wrap items-center justify-between gap-3 rounded-lg border bg-surface px-3 py-2.5"
      >
        <div class="flex min-w-0 items-center gap-2">
          <Server class="size-3.5 shrink-0 text-muted-foreground" />
          <p class="min-w-0 text-sm">
            {{ $t("desktop.settings.server") }}
            <span v-if="current" class="block truncate text-xs text-muted-foreground">
              {{ current }}
            </span>
          </p>
        </div>
        <NuxtLink :to="localeRoute({ name: 'servers' })" class="btn btn-outline">
          {{ $t("desktop.settings.switchServer") }}
        </NuxtLink>
      </li>

      <li
        class="flex flex-wrap items-center justify-between gap-3 rounded-lg border bg-surface px-3 py-2.5"
      >
        <div class="flex min-w-0 items-start gap-2">
          <Bell class="mt-0.5 size-3.5 shrink-0 text-muted-foreground" />
          <p class="min-w-0 text-sm">
            {{ $t("desktop.settings.notifications") }}
            <span class="block text-xs text-muted-foreground">
              {{
                notifications.allowed.value
                  ? $t("desktop.settings.notificationsOn")
                  : $t("desktop.settings.notificationsOff")
              }}
            </span>
          </p>
        </div>
        <Button v-if="!notifications.allowed.value" variant="outline" @click="turnOnNotifications">
          {{ $t("desktop.settings.turnOnNotifications") }}
        </Button>
      </li>
    </ul>

    <p v-if="refused" role="alert" class="mt-2 text-xs text-warning">
      {{ $t("desktop.settings.notificationsRefused") }}
    </p>

    <ul class="mt-3 space-y-px">
      <li v-for="screen in screens" :key="screen.name">
        <NuxtLink
          :to="localeRoute({ name: screen.name })"
          class="flex items-center gap-2 rounded-lg px-2 py-2 text-sm transition-colors hover:bg-accent"
        >
          <component :is="screen.icon" class="size-3.5 text-muted-foreground" />
          <span class="flex-1">{{ screen.label }}</span>
          <ChevronRight class="size-3.5 text-muted-foreground" />
        </NuxtLink>
      </li>
    </ul>
  </section>
</template>
