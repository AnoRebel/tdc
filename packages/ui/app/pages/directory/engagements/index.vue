<script setup lang="ts">
import type { Engagement } from "@tdc/types";
import { isApiError } from "@tdc/types";
import { ArrowLeft, Handshake } from "lucide-vue-next";
import { whenBriefly } from "#layers/ui/app/lib/dates";

const { $t, $ts, $localeRoute: localeRoute } = useI18n();

/** Somebody's engagements, most recently changed first. */

definePageMeta({ middleware: "auth", layout: "app" });

useHead({ title: $ts("directory.engagement.title") });

const directory = useDirectory();
const engagements = ref<Engagement[]>([]);
const error = ref<string | null>(null);
const loaded = ref(false);

try {
  engagements.value = await directory.engagements();
} catch (cause) {
  error.value = isApiError(cause) ? cause.message : $ts("directory.couldNotReach");
} finally {
  loaded.value = true;
}
</script>

<template>
  <div class="flex-1 overflow-y-auto">
    <header class="flex h-12 items-center gap-3 border-b px-4">
      <NuxtLink
        :to="localeRoute({ name: 'directory' })"
        class="btn btn-ghost size-7 px-0"
        :aria-label="$ts('directory.backToDirectory')"
      >
        <ArrowLeft class="size-4" />
      </NuxtLink>
      <h1 class="text-sm font-semibold">{{ $t("directory.engagement.title") }}</h1>
    </header>

    <div class="max-w-3xl space-y-4 px-4 py-6">
      <p
        v-if="error"
        class="rounded-md bg-destructive/10 px-3 py-2 text-sm text-destructive"
        role="alert"
      >
        {{ error }}
      </p>
      <div
        v-if="loaded && !error && !engagements.length"
        class="rounded-lg border border-dashed py-12 text-center"
      >
        <Handshake class="mx-auto size-6 text-muted-foreground/60" />
        <p class="mt-2 text-sm font-medium">{{ $t("directory.engagement.none") }}</p>
        <p class="mt-0.5 text-xs text-muted-foreground">
          {{ $t("directory.engagement.noneHint") }}
        </p>
      </div>
      <ul class="space-y-2">
        <li v-for="engagement in engagements" :key="engagement.id">
          <NuxtLink
            :to="localeRoute({ name: 'directory-engagements-id', params: { id: engagement.id } })"
            class="block rounded-lg border p-3 hover:bg-accent"
          >
            <span class="flex flex-wrap items-center gap-2">
              <span class="text-sm font-medium">{{ engagement.title }}</span>
              <span class="rounded-full bg-muted px-2 py-0.5 text-[0.6875rem]">
                {{ $t(`directory.engagement.states.${engagement.state}`) }}
              </span>
              <time class="ml-auto text-xs text-muted-foreground" :datetime="engagement.updatedAt">
                {{ whenBriefly(engagement.updatedAt) }}
              </time>
            </span>
            <span class="mt-0.5 block text-xs text-muted-foreground">
              {{
                $t("directory.engagement.with", {
                  name:
                    engagement.you === "client"
                      ? engagement.developer.displayName
                      : engagement.client.displayName,
                })
              }}
            </span>
          </NuxtLink>
        </li>
      </ul>
    </div>
  </div>
</template>
