<script setup lang="ts">
import { isApiError, type Cycle, type Issue, type Project } from "@tdc/types";
import { Button } from "#layers/ui/app/components/ui/button";
import { fully, onCalendarDay } from "#layers/ui/app/lib/dates";

const { $t, $ts, $localeRoute: localeRoute } = useI18n();

/**
 * One cycle: its days, how much of it is done, and what is in it.
 *
 * An ended cycle says how its end was dealt with — carried forward or left —
 * so somebody looking at unfinished work in it can tell whether it stayed on
 * purpose.
 */

definePageMeta({ middleware: "auth", layout: "app" });

const route = useRoute();
const router = useRouter();
const key = computed(() => String(route.params.key).toUpperCase());
const id = computed(() => String(route.params.cycle));
const projectsApi = useProjects();

const project = ref<Project | null>(null);
const cycle = ref<Cycle | null>(null);
const issues = ref<Issue[]>([]);
const missing = ref(false);
const failed = ref<string | null>(null);

useHead({ title: computed(() => cycle.value?.name ?? $ts("projects.tabs.cycles")) });

async function load() {
  failed.value = null;
  try {
    const [found, detail] = await Promise.all([
      projectsApi.get(key.value),
      projectsApi.cycle(key.value, id.value),
    ]);
    project.value = found;
    cycle.value = detail.cycle;
    issues.value = detail.issues;
  } catch (cause) {
    if (isApiError(cause) && cause.status === 404) missing.value = true;
    else failed.value = isApiError(cause) ? cause.message : $ts("projects.couldNotLoad");
  }
}
onMounted(load);

async function remove() {
  failed.value = null;
  try {
    await projectsApi.deleteCycle(key.value, id.value);
    await router.push(localeRoute({ name: "projects-key-cycles", params: { key: key.value } }));
  } catch (cause) {
    failed.value = isApiError(cause) ? cause.message : $ts("projects.couldNotSave");
  }
}
</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col">
    <div v-if="missing" class="grid flex-1 place-items-center p-6 text-sm">
      {{ $t("projects.planning.cycleNotFound") }}
    </div>
    <template v-else-if="project && cycle">
      <ProjectsProjectHeader :project="project" current="cycles" />
      <div class="flex-1 space-y-4 overflow-y-auto px-4 py-4">
        <section class="max-w-3xl space-y-2" :aria-label="cycle.name">
          <div class="flex flex-wrap items-center gap-2">
            <h2 class="text-base font-semibold">{{ cycle.name }}</h2>
            <span
              class="rounded border px-1.5 py-0.5 text-[0.65rem] font-medium"
              :data-status="cycle.status"
            >
              {{ $t(`projects.planning.status.${cycle.status}`) }}
            </span>
            <Button
              v-if="project.can?.plan && !issues.length"
              variant="danger"
              class="ml-auto"
              @click="remove"
            >
              {{ $t("projects.planning.removeCycle") }}
            </Button>
          </div>
          <p class="text-sm tabular-nums">
            {{ onCalendarDay(cycle.startsOn) }} – {{ onCalendarDay(cycle.endsOn) }}
            <span class="text-xs text-muted-foreground">({{ project.timeZone }})</span>
          </p>
          <ProjectsProgressReport :project-key="project.key" :of="{ cycle: cycle.id }" />
          <p v-if="cycle.closedAt" class="text-xs text-muted-foreground" data-closed>
            {{
              $t("projects.planning.closed", {
                when: fully(cycle.closedAt),
                policy: $ts(
                  `projects.planning.policies.${cycle.closedPolicy ?? project.cyclePolicy}`,
                ),
              })
            }}
          </p>
        </section>

        <p v-if="failed" class="text-sm text-destructive" role="alert">{{ failed }}</p>

        <section class="max-w-3xl" :aria-label="$ts('projects.planning.issuesInCycle')">
          <h3 class="mb-2 text-xs font-semibold text-muted-foreground uppercase">
            {{ $t("projects.planning.issuesInCycle") }}
          </h3>
          <p v-if="!issues.length" class="text-sm text-muted-foreground">
            {{ $t("projects.planning.emptyCycle") }}
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
              <ProjectsStateBadge :state="issue.state" />
            </li>
          </ul>
        </section>
      </div>
    </template>
    <p v-else-if="failed" class="p-4 text-sm text-destructive" role="alert">{{ failed }}</p>
    <div v-else class="space-y-2 p-4" aria-busy="true">
      <UiSkeleton v-for="n in 3" :key="n" class="h-9 w-full rounded-md" />
    </div>
  </div>
</template>
