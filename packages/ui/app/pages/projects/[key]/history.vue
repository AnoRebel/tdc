<script setup lang="ts">
import { isApiError, type Project, type ProjectHistoryEntry } from "@tdc/types";
import { Button } from "#layers/ui/app/components/ui/button";

const { $t, $ts } = useI18n();

/**
 * What happened to a project itself, newest first, a page at a time.
 *
 * Read from the project's event log, the same log every issue's history comes
 * from; what happened to one issue is on that issue's page, so it is not
 * repeated here.
 */

definePageMeta({ middleware: "auth", layout: "app" });

const route = useRoute();
const key = computed(() => String(route.params.key).toUpperCase());
const projectsApi = useProjects();

/** One page, as the server sends it when no limit is asked for. */
const pageSize = 50;

const project = ref<Project | null>(null);
const entries = ref<ProjectHistoryEntry[]>([]);
const more = ref(false);
const loading = ref(false);
const missing = ref(false);
const failed = ref<string | null>(null);

useHead({
  title: computed(() => `${project.value?.name ?? key.value} · ${$ts("projects.tabs.history")}`),
});

async function load(before?: number) {
  loading.value = true;
  try {
    const page = await projectsApi.projectHistory(key.value, before);
    entries.value = before ? [...entries.value, ...page] : page;
    more.value = page.length === pageSize;
  } catch (cause) {
    failed.value = isApiError(cause) ? cause.message : $ts("projects.projectHistory.couldNotLoad");
  } finally {
    loading.value = false;
  }
}

onMounted(async () => {
  try {
    project.value = await projectsApi.get(key.value);
  } catch (cause) {
    if (isApiError(cause) && cause.status === 404) missing.value = true;
    else failed.value = isApiError(cause) ? cause.message : $ts("projects.couldNotLoad");
    return;
  }
  await load();
});
</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col">
    <div v-if="missing" class="grid flex-1 place-items-center p-6 text-sm">
      {{ $t("projects.notFound") }}
    </div>
    <template v-else-if="project">
      <ProjectsProjectHeader :project="project" current="history" />
      <div class="flex-1 overflow-y-auto px-4 py-6">
        <section class="max-w-3xl space-y-3" aria-labelledby="project-history">
          <div>
            <h2 id="project-history" class="text-sm font-semibold">
              {{ $t("projects.projectHistory.title") }}
            </h2>
            <p class="text-xs text-muted-foreground">{{ $t("projects.projectHistory.hint") }}</p>
          </div>
          <p v-if="failed" class="text-sm text-destructive" role="alert">{{ failed }}</p>
          <p v-else-if="!loading && !entries.length" class="text-sm text-muted-foreground">
            {{ $t("projects.projectHistory.empty") }}
          </p>
          <ProjectsProjectHistory :entries="entries" />
          <Button
            v-if="more"
            variant="ghost"
            :disabled="loading"
            @click="load(entries[entries.length - 1]?.sequence)"
          >
            {{ $t("projects.projectHistory.older") }}
          </Button>
        </section>
      </div>
    </template>
    <p v-else-if="failed" class="p-4 text-sm text-destructive" role="alert">{{ failed }}</p>
  </div>
</template>
