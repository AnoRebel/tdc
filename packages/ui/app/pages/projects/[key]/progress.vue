<script setup lang="ts">
import { isApiError, type Cycle, type Milestone, type Project } from "@tdc/types";
import { onCalendarDay } from "#layers/ui/app/lib/dates";

const { $t, $ts, $localeRoute: localeRoute } = useI18n();

/**
 * How far along a project is: the whole of it over the last two weeks, and
 * each cycle and milestone in it.
 *
 * Every figure here is counted when the page asks — the project's from its
 * issues, its trend from its history — so the page is never behind the board.
 */

definePageMeta({ middleware: "auth", layout: "app" });

const route = useRoute();
const key = computed(() => String(route.params.key).toUpperCase());
const projectsApi = useProjects();

const project = ref<Project | null>(null);
const cycles = ref<Cycle[]>([]);
const milestones = ref<Milestone[]>([]);
const missing = ref(false);
const failed = ref<string | null>(null);

useHead({
  title: computed(() => `${project.value?.name ?? key.value} · ${$ts("projects.tabs.progress")}`),
});

onMounted(async () => {
  try {
    const [found, planned, aims] = await Promise.all([
      projectsApi.get(key.value),
      projectsApi.cycles(key.value),
      projectsApi.milestones(key.value),
    ]);
    project.value = found;
    // Under way first, then those to come, then what has ended — most recent first.
    const order = { active: 0, upcoming: 1, ended: 2 } as const;
    cycles.value = [...planned].sort(
      (a, b) => order[a.status] - order[b.status] || b.startsOn.localeCompare(a.startsOn),
    );
    milestones.value = aims;
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
      <ProjectsProjectHeader :project="project" current="progress" />
      <div class="flex-1 space-y-8 overflow-y-auto px-4 py-6">
        <section class="max-w-3xl space-y-2" :aria-labelledby="'progress-project'">
          <h2 id="progress-project" class="text-sm font-semibold">
            {{ $t("projects.progress.wholeProject") }}
          </h2>
          <ProjectsProgressReport :project-key="project.key" />
        </section>

        <section
          v-if="cycles.length"
          class="max-w-3xl space-y-4"
          :aria-labelledby="'progress-cycles'"
        >
          <h2 id="progress-cycles" class="text-sm font-semibold">
            {{ $t("projects.tabs.cycles") }}
          </h2>
          <article
            v-for="cycle in cycles"
            :key="cycle.id"
            class="space-y-2 rounded-xl border bg-card p-3"
            :aria-label="cycle.name"
            :data-cycle="cycle.name"
          >
            <div class="flex flex-wrap items-center gap-2">
              <NuxtLink
                :to="
                  localeRoute({
                    name: 'projects-key-cycles-cycle',
                    params: { key: project.key, cycle: cycle.id },
                  })
                "
                class="text-sm font-medium hover:underline"
              >
                {{ cycle.name }}
              </NuxtLink>
              <span class="text-xs text-muted-foreground tabular-nums">
                {{ onCalendarDay(cycle.startsOn) }} – {{ onCalendarDay(cycle.endsOn) }}
              </span>
              <span class="rounded border px-1.5 py-0.5 text-[0.65rem] font-medium">
                {{ $t(`projects.planning.status.${cycle.status}`) }}
              </span>
            </div>
            <ProjectsProgressReport
              :project-key="project.key"
              :of="{ cycle: cycle.id }"
              :compact="cycle.status !== 'active'"
            />
          </article>
        </section>

        <section
          v-if="milestones.length"
          class="max-w-3xl space-y-2"
          :aria-labelledby="'progress-milestones'"
        >
          <h2 id="progress-milestones" class="text-sm font-semibold">
            {{ $t("projects.tabs.milestones") }}
          </h2>
          <ul class="divide-y rounded-lg border bg-surface">
            <li
              v-for="milestone in milestones"
              :key="milestone.id"
              class="flex flex-wrap items-center gap-3 px-3 py-2"
            >
              <NuxtLink
                :to="
                  localeRoute({
                    name: 'projects-key-milestones-milestone',
                    params: { key: project.key, milestone: milestone.id },
                  })
                "
                class="min-w-0 flex-1 truncate text-sm font-medium hover:underline"
              >
                {{ milestone.name }}
              </NuxtLink>
              <ProjectsOverdueBadge v-if="milestone.overdue" />
              <ProjectsPlanProgress :completed="milestone.completed" :total="milestone.total" />
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
