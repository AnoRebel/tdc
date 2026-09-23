<script setup lang="ts">
import { useForm } from "@tanstack/vue-form";
import type { AnyFieldApi } from "@tanstack/vue-form";
import * as v from "valibot";
import { h, resolveComponent } from "vue";

import { isApiError, type Issue, type Project } from "@tdc/types";
import { Button } from "#layers/ui/app/components/ui/button";
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
const filing = ref(false);

useHead({ title: computed(() => project.value?.name ?? key.value) });

async function load() {
  failed.value = null;
  try {
    project.value = await projectsApi.get(key.value);
    issues.value = await projectsApi.issues(key.value, {
      state: stateFilter.value === "all" ? undefined : stateFilter.value,
      limit: 200,
    });
  } catch (cause) {
    if (isApiError(cause) && cause.status === 404) missing.value = true;
    else failed.value = isApiError(cause) ? cause.message : $ts("projects.couldNotLoad");
  }
}
onMounted(load);
watch(stateFilter, load);

const NuxtLink = resolveComponent("NuxtLink");
const StateBadge = resolveComponent("ProjectsStateBadge");

/*
 * The columns. The identifier and title link to the issue, because a row is
 * something somebody opens far more often than something they sort.
 */
const columns = [
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
    id: "state",
    accessorFn: (issue: Issue) => issue.state.position,
    header: $ts("projects.state"),
    cell: ({ row }: { row: { original: Issue } }) => h(StateBadge, { state: row.original.state }),
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

        <Field :label="$ts('projects.filterByState')" class="w-56">
          <template #default="{ id }">
            <Select
              :id="id"
              v-model="stateFilter"
              :label="$ts('projects.filterByState')"
              :options="stateOptions"
            />
          </template>
        </Field>

        <p v-if="failed" class="text-sm text-destructive" role="alert">{{ failed }}</p>

        <UiDataTable
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
