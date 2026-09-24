<script setup lang="ts">
import { ArrowLeft } from "lucide-vue-next";

/**
 * Integrations: the programs this person has pointed at the server, and the
 * ones pointed at it on their behalf.
 *
 * A page of its own rather than a section of settings, because what is here is
 * a list somebody manages — each entry with a history worth reading — rather
 * than a preference somebody sets once.
 */

definePageMeta({ middleware: "auth", layout: "app" });

const { $t, $ts, $localeRoute: localeRoute } = useI18n();

useHead({ title: $ts("integrations.title") });
</script>

<template>
  <div class="flex-1 overflow-y-auto">
    <header class="flex h-12 shrink-0 items-center gap-3 border-b px-4">
      <NuxtLink
        :to="localeRoute({ name: 'settings' })"
        class="btn btn-ghost size-7 px-0"
        :aria-label="$ts('integrations.backToSettings')"
      >
        <ArrowLeft class="size-4" aria-hidden="true" />
      </NuxtLink>
      <h1 class="text-sm font-semibold">{{ $t("integrations.title") }}</h1>
    </header>

    <div class="mx-auto max-w-3xl space-y-10 px-4 py-6">
      <IntegrationsOutgoingWebhooks />
    </div>
  </div>
</template>
