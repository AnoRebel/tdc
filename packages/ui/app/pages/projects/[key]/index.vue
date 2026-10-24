<script setup lang="ts">
import { nameOf } from "#layers/ui/app/lib/names";
import { useForm } from "@tanstack/vue-form";
import type { AnyFieldApi } from "@tanstack/vue-form";
import * as v from "valibot";
import { h, resolveComponent } from "vue";

import {
  isApiError,
  type Issue,
  type IssueSearch,
  type Project,
  type ViewFilter,
} from "@tdc/types";
import { Button } from "#layers/ui/app/components/ui/button";
import { Checkbox } from "#layers/ui/app/components/ui/checkbox";
import { Field } from "#layers/ui/app/components/ui/field";
import { Input } from "#layers/ui/app/components/ui/input";
import { Select } from "#layers/ui/app/components/ui/select";
import { Textarea } from "#layers/ui/app/components/ui/textarea";
import { whenBriefly } from "#layers/ui/app/lib/dates";

const { $t, $ts, $localeRoute: localeRoute } = useI18n();

/**
 * A project's issues, as a list.
 *
 * The list is the vendored data table — sorting and paging are the same
 * problem on every screen that lists things, and solving it once is what keeps
 * them behaving the same. The server pages; this shows what it sent.
 */

definePageMeta({ middleware: "auth", layout: "app" });

const route = useRoute();
const router = useRouter();
const key = computed(() => String(route.params.key).toUpperCase());
const projectsApi = useProjects();

const project = ref<Project | null>(null);
const issues = ref<Issue[]>([]);
const missing = ref(false);
const failed = ref<string | null>(null);
const stateFilter = ref<string>("all");
const labelFilter = ref<string>("all");
const sort = ref<"newest" | "priority">("newest");
const filing = ref(false);

useHead({ title: computed(() => project.value?.name ?? key.value) });

/** "all" means no filter; anything else is an identifier to narrow by. */
function narrowing(value: string): string | undefined {
  return value === "all" ? undefined : value;
}

async function load() {
  failed.value = null;
  try {
    project.value = await projectsApi.get(key.value);
    issues.value = await projectsApi.issues(key.value, {
      state: narrowing(stateFilter.value),
      label: narrowing(labelFilter.value),
      sort: sort.value,
      limit: 200,
    });
  } catch (cause) {
    if (isApiError(cause) && cause.status === 404) missing.value = true;
    else failed.value = isApiError(cause) ? cause.message : $ts("projects.couldNotLoad");
  }
}
onMounted(load);
watch([stateFilter, labelFilter, sort], load);

/*
 * Words narrow the list too. A search goes to the search index rather than the
 * list, so its results are the search's — ranked by recency, with the matched
 * words marked — and the state and label chosen above still apply, by name.
 */
const words = ref("");
const found = ref<IssueSearch | null>(null);
watchDebounced(
  [words, stateFilter, labelFilter],
  async () => {
    const text = words.value.trim();
    if (!text) {
      found.value = null;
      return;
    }
    const stateName = project.value?.states?.find((s) => s.id === stateFilter.value)?.name;
    const labelName = project.value?.labels?.find((l) => l.id === labelFilter.value)?.name;
    try {
      found.value = await projectsApi.search({
        q: text,
        project: key.value,
        state: stateName,
        label: labelName,
      });
    } catch (cause) {
      failed.value = isApiError(cause) ? cause.message : $ts("projects.search.failed");
    }
  },
  { debounce: 250 },
);

/*
 * Selecting issues for one change to all of them. The selection is of
 * identifiers, so it survives the list reloading after the change — what was
 * chosen stays chosen while the report is read.
 */
const selected = ref<string[]>([]);
const allSelected = computed(
  () =>
    issues.value.length > 0 && issues.value.every((issue) => selected.value.includes(issue.key)),
);
function choose(key: string, chosen: boolean) {
  selected.value = chosen
    ? [...new Set([...selected.value, key])]
    : selected.value.filter((existing) => existing !== key);
}
function chooseAll(chosen: boolean) {
  selected.value = chosen ? issues.value.map((issue) => issue.key) : [];
}

/** What is on the screen, as a view would ask for it: by name, not by id. */
const currentFilter = computed<ViewFilter>(() => ({
  q: words.value.trim() || undefined,
  state: project.value?.states?.find((s) => s.id === stateFilter.value)?.name,
  label: project.value?.labels?.find((l) => l.id === labelFilter.value)?.name,
  sort: sort.value,
}));

const NuxtLink = resolveComponent("NuxtLink");
const StateBadge = resolveComponent("ProjectsStateBadge");
const PriorityBadge = resolveComponent("ProjectsPriorityBadge");
const LabelChip = resolveComponent("ProjectsLabelChip");
const Avatar = resolveComponent("UiAvatar");

/*
 * The columns. The identifier and title link to the issue, because a row is
 * something somebody opens far more often than something they sort.
 */
const columns = [
  {
    id: "select",
    header: () =>
      h(Checkbox, {
        modelValue: allSelected.value,
        label: $ts("projects.bulk.selectAll"),
        "onUpdate:modelValue": (chosen: boolean) => chooseAll(chosen),
      }),
    cell: ({ row }: { row: { original: Issue } }) =>
      h(Checkbox, {
        modelValue: selected.value.includes(row.original.key),
        label: $ts("projects.bulk.select", { issue: row.original.key }),
        "onUpdate:modelValue": (chosen: boolean) => choose(row.original.key, chosen),
      }),
  },
  {
    accessorKey: "number",
    header: $ts("projects.issue"),
    cell: ({ row }: { row: { original: Issue } }) =>
      h(
        NuxtLink,
        {
          to: localeRoute({ name: "issues-ref", params: { ref: row.original.key } }),
          class: "font-mono text-xs text-muted-foreground hover:text-foreground",
        },
        () => row.original.key,
      ),
  },
  {
    accessorKey: "title",
    header: $ts("projects.issueTitle"),
    cell: ({ row }: { row: { original: Issue } }) =>
      h(
        NuxtLink,
        {
          to: localeRoute({ name: "issues-ref", params: { ref: row.original.key } }),
          class: "font-medium hover:underline",
        },
        () => row.original.title,
      ),
  },
  {
    id: "labels",
    header: $ts("projects.labels"),
    cell: ({ row }: { row: { original: Issue } }) =>
      h(
        "span",
        { class: "flex flex-wrap gap-1" },
        row.original.labels.map((label) => h(LabelChip, { key: label.id, label })),
      ),
  },
  {
    id: "state",
    accessorFn: (issue: Issue) => issue.state.position,
    header: $ts("projects.state"),
    cell: ({ row }: { row: { original: Issue } }) => h(StateBadge, { state: row.original.state }),
  },
  {
    id: "priority",
    // Unprioritised issues sort after every prioritised one, as the server's
    // own priority order does.
    accessorFn: (issue: Issue) => issue.priority?.rank ?? Number.MAX_SAFE_INTEGER,
    header: $ts("projects.priority.title"),
    cell: ({ row }: { row: { original: Issue } }) =>
      h(PriorityBadge, { priority: row.original.priority, compact: false }),
  },
  {
    id: "assignees",
    header: $ts("projects.assignees"),
    cell: ({ row }: { row: { original: Issue } }) =>
      h(
        "span",
        { class: "flex -space-x-1" },
        row.original.assignees.map((person) =>
          h(Avatar, {
            key: person.username,
            name: nameOf(person),
            size: "sm",
          }),
        ),
      ),
  },
  {
    accessorKey: "updatedAt",
    header: $ts("projects.updated"),
    cell: ({ row }: { row: { original: Issue } }) => whenBriefly(row.original.updatedAt),
  },
];

const stateOptions = computed(() => [
  { value: "all", label: $ts("projects.allStates") },
  ...(project.value?.states ?? []).map((state) => ({ value: state.id, label: state.name })),
]);
const labelOptions = computed(() => [
  { value: "all", label: $ts("projects.allLabels") },
  ...(project.value?.labels ?? []).map((label) => ({ value: label.id, label: label.name })),
]);
const sortOptions = computed(() => [
  { value: "newest" as const, label: $ts("projects.sort.newest") },
  { value: "priority" as const, label: $ts("projects.sort.priority") },
]);

const schema = v.object({
  title: v.pipe(
    v.string(),
    v.trim(),
    v.minLength(1, $ts("projects.titleRequired")),
    v.maxLength(200, $ts("projects.titleTooLong")),
  ),
  description: v.pipe(v.string(), v.maxLength(20000)),
});

const refused = ref<string | null>(null);
const form = useForm({
  defaultValues: { title: "", description: "" },
  validators: { onSubmit: schema },
  async onSubmit({ value }) {
    refused.value = null;
    try {
      const issue = await projectsApi.file(key.value, {
        title: value.title.trim(),
        description: value.description.trim(),
      });
      await router.push(localeRoute({ name: "issues-ref", params: { ref: issue.key } }));
    } catch (cause) {
      refused.value = isApiError(cause) ? cause.message : $ts("projects.couldNotFile");
    }
  },
});

// N files a new issue here, for whoever may.
useShortcutAction("newIssue", () => {
  if (!project.value?.can?.createIssues) return false;
  filing.value = true;
  form.reset();
});

function firstError(field: AnyFieldApi): string | undefined {
  if (!field.state.meta.isTouched) return undefined;
  const [issue] = field.state.meta.errors;
  if (!issue) return undefined;
  return typeof issue === "string" ? issue : (issue as { message?: string }).message;
}
</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col">
    <div v-if="missing" class="grid flex-1 place-items-center p-6 text-center">
      <div>
        <p class="text-sm font-medium">{{ $t("projects.notFound") }}</p>
        <NuxtLink
          :to="localeRoute({ name: 'projects' })"
          class="mt-2 inline-block text-xs text-primary hover:underline"
        >
          {{ $t("projects.all") }}
        </NuxtLink>
      </div>
    </div>

    <template v-else-if="project">
      <ProjectsProjectHeader :project="project" current="issues">
        <template #actions>
          <Button
            v-if="project.can?.createIssues && !filing"
            class="ml-auto"
            @click="((filing = true), form.reset())"
          >
            <Icon name="lucide:plus" class="size-3.5" />
            {{ $t("projects.fileIssue") }}
          </Button>
        </template>
      </ProjectsProjectHeader>

      <div class="flex-1 space-y-4 overflow-y-auto px-4 py-4">
        <section
          v-if="filing"
          class="max-w-2xl rounded-xl border bg-card p-4"
          :aria-label="$ts('projects.fileIssue')"
        >
          <form class="space-y-3" @submit.prevent="form.handleSubmit()">
            <form.Field name="title">
              <template #default="{ field }">
                <Field :label="$ts('projects.issueTitle')" :error="firstError(field)" required>
                  <template #default="{ id, invalid, describedBy }">
                    <Input
                      :id="id"
                      :model-value="field.state.value"
                      :invalid="invalid"
                      :described-by="describedBy"
                      :placeholder="$ts('projects.titlePlaceholder')"
                      @update:model-value="field.handleChange($event)"
                      @blur="field.handleBlur"
                    />
                  </template>
                </Field>
              </template>
            </form.Field>
            <form.Field name="description">
              <template #default="{ field }">
                <Field :label="$ts('projects.description')" :error="firstError(field)">
                  <template #default="{ id, invalid, describedBy }">
                    <Textarea
                      :id="id"
                      class="h-auto py-2"
                      rows="4"
                      :model-value="field.state.value"
                      :invalid="invalid"
                      :described-by="describedBy"
                      @update:model-value="field.handleChange($event)"
                      @blur="field.handleBlur"
                    />
                  </template>
                </Field>
              </template>
            </form.Field>
            <p v-if="refused" class="text-sm text-destructive" role="alert">{{ refused }}</p>
            <div class="flex justify-end gap-2">
              <Button variant="ghost" @click="filing = false">{{ $t("action.cancel") }}</Button>
              <Button type="submit">{{ $t("projects.fileIssue") }}</Button>
            </div>
          </form>
        </section>

        <div class="flex flex-wrap items-end gap-3" role="search">
          <Field :label="$ts('projects.search.words')" class="min-w-48 flex-1">
            <template #default="{ id }">
              <Input
                :id="id"
                v-model="words"
                type="search"
                :placeholder="$ts('projects.search.inProject')"
              />
            </template>
          </Field>
          <Field :label="$ts('projects.filterByState')" class="w-48">
            <template #default="{ id }">
              <Select
                :id="id"
                v-model="stateFilter"
                :label="$ts('projects.filterByState')"
                :options="stateOptions"
              />
            </template>
          </Field>
          <Field :label="$ts('projects.filterByLabel')" class="w-48">
            <template #default="{ id }">
              <Select
                :id="id"
                v-model="labelFilter"
                :label="$ts('projects.filterByLabel')"
                :options="labelOptions"
              />
            </template>
          </Field>
          <Field :label="$ts('projects.sort.title')" class="w-48">
            <template #default="{ id }">
              <Select
                :id="id"
                v-model="sort"
                :label="$ts('projects.sort.title')"
                :options="sortOptions"
              />
            </template>
          </Field>
        </div>

        <ProjectsProjectViews
          :project-key="project.key"
          layout="list"
          :filter="currentFilter"
          :may-share="Boolean(project.role)"
        />

        <ProjectsBulkActions
          v-if="selected.length && !(found && words.trim())"
          :project="project"
          :selected="selected"
          @applied="load"
          @clear="selected = []"
        />

        <p v-if="failed" class="text-sm text-destructive" role="alert">{{ failed }}</p>

        <ProjectsIssueSearchResults v-if="found && words.trim()" :results="found" />
        <UiDataTable
          v-else
          class="bg-surface"
          :columns="columns"
          :data="issues"
          :caption="$ts('projects.issuesCaption', { project: project.name })"
          :empty="$ts('projects.noIssues')"
        />
      </div>
    </template>

    <div v-else class="space-y-2 p-4" aria-busy="true">
      <UiSkeleton v-for="n in 4" :key="n" class="h-9 w-full rounded-md" />
    </div>
  </div>
</template>
