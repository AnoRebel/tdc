<script setup lang="ts">
import type { Company } from "@tdc/types";
import { ArrowLeft } from "lucide-vue-next";

const { $t, $ts, $localeRoute: localeRoute } = useI18n();

/** Creating a company, whose first administrator is whoever creates it. */

definePageMeta({ middleware: "auth", layout: "app" });

useHead({ title: $ts("directory.company.create") });

const router = useRouter();

async function created(company: Company) {
  await router.push(
    localeRoute({ name: "directory-companies-slug", params: { slug: company.slug } }),
  );
}
</script>

<template>
  <div class="flex-1 overflow-y-auto">
    <header class="flex h-12 items-center gap-3 border-b px-4">
      <NuxtLink
        :to="localeRoute({ name: 'directory-profile' })"
        class="btn btn-ghost size-7 px-0"
        :aria-label="$ts('action.back')"
      >
        <ArrowLeft class="size-4" />
      </NuxtLink>
      <h1 class="text-sm font-semibold">{{ $t("directory.company.create") }}</h1>
    </header>

    <div class="max-w-2xl space-y-4 px-4 py-6">
      <p class="text-sm text-muted-foreground">{{ $t("directory.company.createHint") }}</p>
      <DirectoryCompanyForm @saved="created" />
    </div>
  </div>
</template>
