<script setup lang="ts">
import { isApiError, type SavedView } from "@tdc/types";

const { $t, $ts, $localeRoute: localeRoute } = useI18n();

/**
 * Saved views across projects: the requester's own, and those shared.
 *
 * A project's own views live with the project, on its list and its board;
 * these are the ones that ask of every project at once.
 */

definePageMeta({ middleware: "auth", layout: "app" });
useHead({ title: computed(() => $ts("projects.saved.allViews")) });

const projectsApi = useProjects();
const views = ref<SavedView[] | null>(null);
const failed = ref<string | null>(null);

onMounted(async () => {
  try {
    views.value = await projectsApi.views(null);
  } catch (cause) {
    failed.value = isApiError(cause) ? cause.message : $ts("projects.couldNotLoad");
  }
});
</script>

<template>
  <div class="flex-1 overflow-y-auto px-4 py-6">
    <div class="mx-auto max-w-2xl space-y-4">
      <h1 class="text-lg font-semibold">{{ $t("projects.saved.acrossProjects") }}</h1>
      <p class="text-sm text-muted-foreground">
        <NuxtLink :to="localeRoute({ name: 'issues-search' })" class="text-primary hover:underline">
          {{ $t("projects.search.title") }}
        </NuxtLink>
      </p>
      <p v-if="failed" class="text-sm text-destructive" role="alert">{{ failed }}</p>
      <ProjectsViewList
        v-else-if="views"
        :views="views"
        @removed="(id) => (views = (views ?? []).filter((view) => view.id !== id))"
      />
      <UiSkeleton v-else class="h-24 w-full rounded-lg" />
    </div>
  </div>
</template>
