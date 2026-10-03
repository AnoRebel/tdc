<script setup lang="ts">
import { isApiError, type Cycle, type Issue, type Milestone, type Project } from "@tdc/types";
import { fully, onCalendarDay } from "#layers/ui/app/lib/dates";

const { $t, $ts, $localeRoute: localeRoute } = useI18n();

/**
 * One milestone: its day, how far along it is, the issues that count towards
 * it, and the cycles they are in — which is what it spans.
 */

definePageMeta({ middleware: "auth", layout: "app" });

const route = useRoute();
const key = computed(() => String(route.params.key).toUpperCase());
const id = computed(() => String(route.params.milestone));
const projectsApi = useProjects();

const project = ref<Project | null>(null);
const milestone = ref<Milestone | null>(null);
const issues = ref<Issue[]>([]);
const cycles = ref<Cycle[]>([]);
const missing = ref(false);
const failed = ref<string | null>(null);

useHead({ title: computed(() => milestone.value?.name ?? $ts("projects.tabs.milestones")) });

onMounted(async () => {
  try {
    const [found, detail] = await Promise.all([
      projectsApi.get(key.value),
      projectsApi.milestone(key.value, id.value),
    ]);
    project.value = found;
    milestone.value = detail.milestone;
    issues.value = detail.issues;
    cycles.value = detail.cycles;
  } catch (cause) {
    if (isApiError(cause) && cause.status === 404) missing.value = true;
    else failed.value = isApiError(cause) ? cause.message : $ts("projects.couldNotLoad");
  }
});
</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col">
    <div v-if="missing" class="grid flex-1 place-items-center p-6 text-sm">
      {{ $t("projects.planning.milestoneNotFound") }}
    </div>
    <p v-else-if="failed" class="p-4 text-sm text-destructive" role="alert">{{ failed }}</p>
    <template v-else-if="project && milestone">
      <ProjectsProjectHeader :project="project" current="milestones" />
      <div class="flex-1 space-y-4 overflow-y-auto px-4 py-4">
        <section class="max-w-3xl space-y-2" :aria-label="milestone.name">
          <div class="flex flex-wrap items-center gap-2">
            <h2 class="text-base font-semibold">{{ milestone.name }}</h2>
            <ProjectsOverdueBadge v-if="milestone.overdue" />
          </div>
          <p v-if="milestone.description" class="text-sm">{{ milestone.description }}</p>
          <p v-if="milestone.targetDate" class="text-sm tabular-nums">
            {{ $t("projects.planning.target", { day: onCalendarDay(milestone.targetDate) }) }}
            <span v-if="milestone.dueAt" class="text-xs text-muted-foreground">
              ({{ $t("projects.planning.dueAt", { when: fully(milestone.dueAt) }) }})
            </span>
          </p>
          <p v-if="milestone.overdue" class="text-xs text-muted-foreground">
            {{ $t("projects.planning.overdueHint") }}
          </p>
          <ProjectsPlanProgress :completed="milestone.completed" :total="milestone.total" />
        </section>

        <section class="max-w-3xl" :aria-label="$ts('projects.planning.spans')">
          <h3 class="mb-2 text-xs font-semibold text-muted-foreground uppercase">
            {{ $t("projects.planning.spans") }}
          </h3>
          <p v-if="!cycles.length" class="text-sm text-muted-foreground">
            {{ $t("projects.planning.spansNone") }}
          </p>
          <ul v-else class="flex flex-wrap gap-2">
            <li v-for="cycle in cycles" :key="cycle.id">
              <NuxtLink
                :to="
                  localeRoute({
                    name: 'projects-key-cycles-cycle',
                    params: { key: project.key, cycle: cycle.id },
                  })
                "
                class="inline-flex items-center gap-1 rounded-md border bg-surface px-2 py-1 text-xs hover:bg-accent"
              >
                {{ cycle.name }}
                <span class="text-muted-foreground">
                  {{ onCalendarDay(cycle.startsOn) }} – {{ onCalendarDay(cycle.endsOn) }}
                </span>
              </NuxtLink>
            </li>
          </ul>
        </section>

        <section class="max-w-3xl" :aria-label="$ts('projects.planning.issuesInMilestone')">
          <h3 class="mb-2 text-xs font-semibold text-muted-foreground uppercase">
            {{ $t("projects.planning.issuesInMilestone") }}
          </h3>
          <p v-if="!issues.length" class="text-sm text-muted-foreground">
            {{ $t("projects.planning.emptyMilestone") }}
          </p>
          <ul v-else class="divide-y rounded-lg border bg-surface">
            <li v-for="issue in issues" :key="issue.id" class="flex items-center gap-2 px-3 py-2">
              <NuxtLink
                :to="localeRoute({ name: 'issues-ref', params: { ref: issue.key } })"
                class="font-mono text-xs text-muted-foreground hover:text-foreground"
              >
                {{ issue.key }}
              </NuxtLink>
              <span class="min-w-0 flex-1 truncate text-sm">{{ issue.title }}</span>
              <span v-if="issue.cycle" class="text-xs text-muted-foreground">
                {{ issue.cycle.name }}
              </span>
              <ProjectsStateBadge :state="issue.state" />
            </li>
          </ul>
        </section>
      </div>
    </template>
    <div v-else class="space-y-2 p-4" aria-busy="true">
      <UiSkeleton v-for="n in 3" :key="n" class="h-9 w-full rounded-md" />
    </div>
  </div>
</template>
