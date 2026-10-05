<script setup lang="ts">
import { useForm } from "@tanstack/vue-form";
import * as v from "valibot";

import { isApiError, type IssueSearch, type SavedView, type ViewFilter } from "@tdc/types";
import { Button } from "#layers/ui/app/components/ui/button";
import { Field } from "#layers/ui/app/components/ui/field";
import { Input } from "#layers/ui/app/components/ui/input";
import { Select } from "#layers/ui/app/components/ui/select";

const { $t, $ts, $localeRoute: localeRoute } = useI18n();

/**
 * Searching every issue you can see, and what was said about them.
 *
 * Words and filters narrow together. The filters name a state, a label or a
 * person rather than choosing from one project's lists, because they apply
 * across projects: "Done" means done wherever it is. The search lives in the
 * address, so a search somebody found useful is a link they can share — and
 * opening it shows each person what they may see, not what its author saw.
 */

definePageMeta({ middleware: "auth", layout: "app" });
useHead({ title: $ts("projects.search.title") });

const route = useRoute();
const router = useRouter();
const projectsApi = useProjects();

const results = ref<IssueSearch | null>(null);
const searching = ref(false);
const failed = ref<string | null>(null);

const fromQuery = (name: string) =>
  typeof route.query[name] === "string" ? String(route.query[name]) : "";

const form = useForm({
  defaultValues: {
    q: fromQuery("q"),
    state: fromQuery("state"),
    label: fromQuery("label"),
    assignee: fromQuery("assignee"),
    completed: (fromQuery("completed") || "any") as "any" | "true" | "false",
  },
  validators: {
    onSubmit: v.object({
      q: v.pipe(v.string(), v.maxLength(200)),
      state: v.pipe(v.string(), v.maxLength(40)),
      label: v.pipe(v.string(), v.maxLength(30)),
      assignee: v.pipe(v.string(), v.maxLength(64)),
      completed: v.picklist(["any", "true", "false"]),
    }),
  },
  async onSubmit({ value }) {
    const query: Record<string, string> = {};
    for (const [name, entry] of Object.entries(value)) {
      if (entry && entry !== "any") query[name] = entry.trim();
    }
    await router.replace(localeRoute({ name: "issues-search", query }));
  },
});

async function run() {
  const q = fromQuery("q");
  const any = ["q", "state", "label", "assignee", "completed"].some((name) => fromQuery(name));
  if (!any) {
    results.value = null;
    return;
  }
  searching.value = true;
  failed.value = null;
  try {
    const completed = fromQuery("completed");
    results.value = await projectsApi.search({
      q,
      state: fromQuery("state"),
      label: fromQuery("label"),
      assignee: fromQuery("assignee"),
      completed: completed ? completed === "true" : undefined,
    });
  } catch (cause) {
    failed.value = isApiError(cause) ? cause.message : $ts("projects.search.failed");
  } finally {
    searching.value = false;
  }
}
watch(() => route.query, run, { immediate: true });

/*
 * A search worth keeping is saved as a view across projects: the same
 * question, by name, answered afresh for whoever opens it.
 */
const saving = ref(false);
const savedView = ref<SavedView | null>(null);
const asView = computed<ViewFilter>(() => ({
  q: fromQuery("q") || undefined,
  state: fromQuery("state") || undefined,
  label: fromQuery("label") || undefined,
  assignee: fromQuery("assignee") || undefined,
}));

const completions = computed(() => [
  { value: "any" as const, label: $ts("projects.search.anyState") },
  { value: "false" as const, label: $ts("projects.search.open") },
  { value: "true" as const, label: $ts("projects.search.finished") },
]);
</script>

<template>
  <div class="flex-1 overflow-y-auto">
    <header class="flex h-12 shrink-0 items-center gap-3 border-b px-4">
      <h1 class="text-sm font-semibold">{{ $t("projects.search.title") }}</h1>
      <NuxtLink
        :to="localeRoute({ name: 'views' })"
        class="ml-auto text-xs text-primary hover:underline"
      >
        {{ $t("projects.saved.allViews") }}
      </NuxtLink>
    </header>

    <div class="max-w-4xl space-y-6 px-4 py-6">
      <form
        class="grid gap-3 rounded-xl border bg-card p-4 sm:grid-cols-2"
        role="search"
        @submit.prevent="form.handleSubmit()"
      >
        <form.Field name="q">
          <template #default="{ field }">
            <Field :label="$ts('projects.search.words')" class="sm:col-span-2">
              <template #default="{ id }">
                <Input
                  :id="id"
                  type="search"
                  :model-value="field.state.value"
                  :placeholder="$ts('projects.search.wordsPlaceholder')"
                  @update:model-value="field.handleChange($event)"
                />
              </template>
            </Field>
          </template>
        </form.Field>
        <form.Field name="state">
          <template #default="{ field }">
            <Field :label="$ts('projects.state')">
              <template #default="{ id }">
                <Input
                  :id="id"
                  :model-value="field.state.value"
                  :placeholder="$ts('projects.search.statePlaceholder')"
                  @update:model-value="field.handleChange($event)"
                />
              </template>
            </Field>
          </template>
        </form.Field>
        <form.Field name="label">
          <template #default="{ field }">
            <Field :label="$ts('projects.search.label')">
              <template #default="{ id }">
                <Input
                  :id="id"
                  :model-value="field.state.value"
                  placeholder="bug"
                  @update:model-value="field.handleChange($event)"
                />
              </template>
            </Field>
          </template>
        </form.Field>
        <form.Field name="assignee">
          <template #default="{ field }">
            <Field :label="$ts('projects.search.assignee')">
              <template #default="{ id }">
                <Input
                  :id="id"
                  :model-value="field.state.value"
                  :placeholder="$ts('projects.username')"
                  @update:model-value="field.handleChange($event)"
                />
              </template>
            </Field>
          </template>
        </form.Field>
        <form.Field name="completed">
          <template #default="{ field }">
            <Field :label="$ts('projects.search.finishedOrNot')">
              <template #default="{ id }">
                <Select
                  :id="id"
                  :model-value="field.state.value"
                  :label="$ts('projects.search.finishedOrNot')"
                  :options="completions"
                  @update:model-value="$event && field.handleChange($event)"
                />
              </template>
            </Field>
          </template>
        </form.Field>
        <div class="flex justify-end sm:col-span-2">
          <Button type="submit">
            <Icon name="lucide:search" class="size-3.5" />
            {{ $t("action.search") }}
          </Button>
        </div>
      </form>

      <p v-if="failed" class="text-sm text-destructive" role="alert">{{ failed }}</p>
      <div v-else-if="searching" class="space-y-2" aria-busy="true">
        <UiSkeleton v-for="n in 3" :key="n" class="h-12 w-full rounded-md" />
      </div>
      <template v-else-if="results">
        <div class="flex flex-wrap items-center gap-2">
          <!-- A view does not keep "finished or not", so it is not offered
               for a search that relies on it rather than saved without it. -->
          <Button
            v-if="!saving && !fromQuery('completed')"
            variant="outline"
            @click="((saving = true), (savedView = null))"
          >
            <Icon name="lucide:bookmark-plus" class="size-3.5" />
            {{ $t("projects.saved.save") }}
          </Button>
          <span role="status">
            <NuxtLink
              v-if="savedView"
              :to="localeRoute({ name: 'views-id', params: { id: savedView.id } })"
              class="text-sm text-primary hover:underline"
            >
              {{ savedView.name }}
            </NuxtLink>
          </span>
        </div>
        <ProjectsSaveView
          v-if="saving"
          :project-key="null"
          layout="list"
          :filter="asView"
          may-share
          @saved="(view) => ((savedView = view), (saving = false))"
          @cancel="saving = false"
        />
        <ProjectsIssueSearchResults :results="results" show-project />
      </template>
      <p v-else class="text-sm text-muted-foreground">{{ $t("projects.search.hint") }}</p>
    </div>
  </div>
</template>
