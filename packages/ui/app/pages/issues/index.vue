<script setup lang="ts">
import { h, resolveComponent } from "vue";

import { isApiError, type MyIssue } from "@tdc/types";
import { whenBriefly } from "#layers/ui/app/lib/dates";

const { $t, $ts, $localeRoute: localeRoute } = useI18n();

/**
 * Everything assigned to me, across every project I can see.
 *
 * Only projects I can still see: an assignment in a private project I have
 * since left is not mine to look at any more, and the server leaves it out.
 */

definePageMeta({ middleware: "auth", layout: "app" });
useHead({ title: $ts("projects.mine.title") });

const projectsApi = useProjects();
const issues = ref<MyIssue[]>([]);
const loading = ref(true);
const failed = ref<string | null>(null);

onMounted(async () => {
  try {
    issues.value = await projectsApi.mine();
  } catch (cause) {
    failed.value = isApiError(cause) ? cause.message : $ts("projects.couldNotLoad");
  } finally {
    loading.value = false;
  }
});

const NuxtLink = resolveComponent("NuxtLink");
const StateBadge = resolveComponent("ProjectsStateBadge");
const PriorityBadge = resolveComponent("ProjectsPriorityBadge");

const columns = [
  {
    accessorKey: "key",
    header: $ts("projects.issue"),
    cell: ({ row }: { row: { original: MyIssue } }) =>
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
    cell: ({ row }: { row: { original: MyIssue } }) =>
      h(
        NuxtLink,
        {
          to: localeRoute({ name: "issues-ref", params: { ref: row.original.key } }),
          class: "font-medium hover:underline",
        },
        () => row.original.title,
      ),
  },
  { accessorKey: "projectName", header: $ts("projects.mine.project") },
  {
    id: "state",
    accessorFn: (issue: MyIssue) => (issue.state.isCompletion ? 1 : 0),
    header: $ts("projects.state"),
    cell: ({ row }: { row: { original: MyIssue } }) => h(StateBadge, { state: row.original.state }),
  },
  {
    id: "priority",
    accessorFn: (issue: MyIssue) => issue.priority?.rank ?? 99,
    header: $ts("projects.priority.title"),
    cell: ({ row }: { row: { original: MyIssue } }) =>
      h(PriorityBadge, { priority: row.original.priority }),
  },
  {
    accessorKey: "updatedAt",
    header: $ts("projects.updated"),
    cell: ({ row }: { row: { original: MyIssue } }) => whenBriefly(row.original.updatedAt),
  },
];
</script>

<template>
  <div class="flex-1 overflow-y-auto">
    <header class="flex h-12 shrink-0 items-center gap-3 border-b px-4">
      <h1 class="text-sm font-semibold">{{ $t("projects.mine.title") }}</h1>
    </header>
    <div class="max-w-5xl px-4 py-6">
      <p v-if="failed" class="text-sm text-destructive" role="alert">{{ failed }}</p>
      <div v-else-if="loading" class="space-y-2" aria-busy="true">
        <UiSkeleton v-for="n in 3" :key="n" class="h-9 w-full rounded-md" />
      </div>
      <UiDataTable
        v-else
        class="bg-surface"
        :columns="columns"
        :data="issues"
        :caption="$ts('projects.mine.title')"
        :empty="$ts('projects.mine.none')"
      />
    </div>
  </div>
</template>
