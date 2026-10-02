<script setup lang="ts">
import { isApiError, type Project } from "@tdc/types";

const { $t, $ts } = useI18n();

/**
 * A project's board.
 *
 * The page is the project's header and the shared board component; everything
 * about columns, cards and moving them lives in the component, so another
 * client showing a board shows the same one.
 */

definePageMeta({ middleware: "auth", layout: "app" });

const route = useRoute();
const key = computed(() => String(route.params.key).toUpperCase());
const projectsApi = useProjects();

const project = ref<Project | null>(null);
const missing = ref(false);
const failed = ref<string | null>(null);

useHead({
  title: computed(() => `${project.value?.name ?? key.value} · ${$ts("projects.tabs.board")}`),
});

onMounted(async () => {
  try {
    project.value = await projectsApi.get(key.value);
  } catch (cause) {
    if (isApiError(cause) && cause.status === 404) missing.value = true;
    else failed.value = isApiError(cause) ? cause.message : $ts("projects.couldNotLoad");
  }
});
</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col">
    <div v-if="missing" class="grid flex-1 place-items-center p-6 text-sm">
      {{ $t("projects.notFound") }}
    </div>
    <p v-else-if="failed" class="p-4 text-sm text-destructive" role="alert">{{ failed }}</p>
    <template v-else-if="project">
      <ProjectsProjectHeader :project="project" current="board" />
      <ProjectsIssueBoard :project-key="project.key" :can-move="Boolean(project.can?.transition)" />
    </template>
  </div>
</template>
