<script setup lang="ts">
import { isApiError, type BoardFilter, type Project } from "@tdc/types";
import { Field } from "#layers/ui/app/components/ui/field";
import { Input } from "#layers/ui/app/components/ui/input";
import { Select } from "#layers/ui/app/components/ui/select";

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

/*
 * What the board shows. By name, as a saved view keeps it, so saving the board
 * as it is now saves exactly this.
 */
const label = ref("any");
const priority = ref("any");
const assignee = ref("");
const assigneeSettled = refDebounced(assignee, 300);
const filter = computed<BoardFilter>(() => ({
  label: label.value === "any" ? undefined : label.value,
  priority: priority.value === "any" ? undefined : priority.value,
  assignee: assigneeSettled.value.trim() || undefined,
}));
const labelOptions = computed(() => [
  { value: "any", label: $ts("projects.board.anyLabel") },
  ...(project.value?.labels ?? []).map((entry) => ({ value: entry.name, label: entry.name })),
]);
const priorityOptions = computed(() => [
  { value: "any", label: $ts("projects.board.anyPriority") },
  ...(project.value?.priorities ?? []).map((entry) => ({ value: entry.name, label: entry.name })),
]);

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
      <div class="space-y-3 border-b px-4 py-3">
        <div
          class="flex flex-wrap items-end gap-3"
          role="search"
          :aria-label="$ts('projects.board.filters')"
        >
          <Field :label="$ts('projects.filterByLabel')" class="w-44">
            <template #default="{ id }">
              <Select
                :id="id"
                v-model="label"
                :label="$ts('projects.filterByLabel')"
                :options="labelOptions"
              />
            </template>
          </Field>
          <Field :label="$ts('projects.priority.title')" class="w-44">
            <template #default="{ id }">
              <Select
                :id="id"
                v-model="priority"
                :label="$ts('projects.priority.title')"
                :options="priorityOptions"
              />
            </template>
          </Field>
          <Field :label="$ts('projects.board.assignee')" class="w-44">
            <template #default="{ id }">
              <Input
                :id="id"
                v-model="assignee"
                :placeholder="$ts('projects.board.assigneePlaceholder')"
              />
            </template>
          </Field>
        </div>
        <ProjectsProjectViews
          :project-key="project.key"
          layout="board"
          :filter="filter"
          :may-share="Boolean(project.role)"
        />
      </div>
      <ProjectsIssueBoard
        :project-key="project.key"
        :can-move="Boolean(project.can?.transition)"
        :filter="filter"
      />
    </template>
  </div>
</template>
