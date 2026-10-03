<script setup lang="ts">
import { isApiError, type OpenedView, type Project } from "@tdc/types";

const { $t, $ts, $localeRoute: localeRoute } = useI18n();

/**
 * A saved view, opened.
 *
 * The server answers it as whoever opens it, so what is here is what this
 * person may see — a view shared by somebody who can see more shows them
 * less, and says so. A board view is its project's board with the view's
 * filters, so cards move here as they would on the board itself.
 */

definePageMeta({ middleware: "auth", layout: "app" });

const route = useRoute();
const projectsApi = useProjects();

const opened = ref<OpenedView | null>(null);
const project = ref<Project | null>(null);
const missing = ref(false);
const failed = ref<string | null>(null);

useHead({ title: computed(() => opened.value?.view.name ?? $ts("projects.saved.title")) });

async function load() {
  failed.value = null;
  missing.value = false;
  try {
    opened.value = await projectsApi.openView(String(route.params.id));
    const key = opened.value.view.project;
    project.value = key ? await projectsApi.get(key) : null;
  } catch (cause) {
    if (isApiError(cause) && cause.status === 404) missing.value = true;
    else failed.value = isApiError(cause) ? cause.message : $ts("projects.saved.couldNotOpen");
  }
}
onMounted(load);
watch(() => route.params.id, load);

/** The view's question, said in words. */
const asks = computed(() => {
  const filter = opened.value?.view.filter ?? {};
  const parts: string[] = [];
  if (filter.q) parts.push($ts("projects.saved.filterWords", { value: filter.q }));
  if (filter.state) parts.push($ts("projects.saved.filterState", { value: filter.state }));
  if (filter.label) parts.push($ts("projects.saved.filterLabel", { value: filter.label }));
  if (filter.priority) parts.push($ts("projects.saved.filterPriority", { value: filter.priority }));
  if (filter.assignee) parts.push($ts("projects.saved.filterAssignee", { value: filter.assignee }));
  return parts.length ? parts.join(", ") : $ts("projects.saved.everything");
});

const boardFilter = computed(() => {
  const filter = opened.value?.view.filter ?? {};
  return { label: filter.label, priority: filter.priority, assignee: filter.assignee };
});
</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col">
    <div v-if="missing" class="grid flex-1 place-items-center p-6 text-center">
      <div>
        <p class="text-sm font-medium">{{ $t("projects.saved.notFound") }}</p>
        <NuxtLink
          :to="localeRoute({ name: 'views' })"
          class="mt-2 inline-block text-xs text-primary hover:underline"
        >
          {{ $t("projects.saved.allViews") }}
        </NuxtLink>
      </div>
    </div>
    <p v-else-if="failed" class="p-4 text-sm text-destructive" role="alert">{{ failed }}</p>

    <template v-else-if="opened">
      <header class="space-y-1 border-b px-4 py-3">
        <div class="flex flex-wrap items-center gap-2">
          <h1 class="text-sm font-semibold">{{ opened.view.name }}</h1>
          <NuxtLink
            v-if="opened.view.project"
            :to="localeRoute({ name: 'projects-key', params: { key: opened.view.project } })"
            class="font-mono text-xs text-muted-foreground hover:text-foreground"
          >
            {{ opened.view.project }}
          </NuxtLink>
          <span
            v-if="opened.view.shared"
            class="rounded border px-1.5 py-0.5 text-[0.65rem] font-medium text-muted-foreground"
          >
            {{ $t("projects.saved.shared") }}
          </span>
          <span v-if="!opened.view.mine" class="text-xs text-muted-foreground">
            {{ $t("projects.saved.by", { owner: opened.view.owner }) }}
          </span>
        </div>
        <p class="text-xs text-muted-foreground">
          {{ $t("projects.saved.asks") }}: <span class="text-foreground">{{ asks }}</span>
        </p>
        <p class="text-xs text-muted-foreground">{{ $t("projects.saved.openedAs") }}</p>
      </header>

      <ProjectsIssueBoard
        v-if="opened.view.layout === 'board' && project"
        :project-key="project.key"
        :can-move="Boolean(project.can?.transition)"
        :filter="boardFilter"
      />
      <div v-else class="flex-1 overflow-y-auto px-4 py-4">
        <ProjectsIssueSearchResults
          :results="{ issues: opened.issues ?? [], discussions: [] }"
          :show-project="!opened.view.project"
        />
      </div>
    </template>

    <div v-else class="space-y-2 p-4" aria-busy="true">
      <UiSkeleton v-for="n in 4" :key="n" class="h-9 w-full rounded-md" />
    </div>
  </div>
</template>
