<script setup lang="ts">
import { Contact, UserRound } from "lucide-vue-next";

const { $t, $ts, $localeRoute: localeRoute } = useI18n();

/**
 * The directory: people who chose to be found, and the companies they agreed
 * to be listed by.
 */

definePageMeta({ middleware: "auth", layout: "app" });

useHead({ title: $ts("directory.title") });
</script>

<template>
  <div class="flex-1 overflow-y-auto">
    <header class="flex h-12 items-center gap-3 border-b px-4">
      <Contact class="size-4 text-muted-foreground" />
      <h1 class="text-sm font-semibold">{{ $t("directory.title") }}</h1>
      <NuxtLink :to="localeRoute({ name: 'directory-profile' })" class="btn btn-ghost ml-auto">
        <UserRound class="size-3.5" />
        {{ $t("directory.profile.yours") }}
      </NuxtLink>
    </header>

    <div class="max-w-3xl px-4 py-6">
      <p class="text-sm text-muted-foreground">{{ $t("directory.intro") }}</p>
    </div>
  </div>
</template>
