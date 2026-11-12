<script setup lang="ts">
import { isApiError, type Progress } from "@tdc/types";
import { useWindowFocus } from "@vueuse/core";
import { Chart } from "#layers/ui/app/components/ui/chart";
import { Skeleton } from "#layers/ui/app/components/ui/skeleton";
import PlanProgress from "#layers/ui/app/components/projects/PlanProgress.vue";
import { onCalendarDay } from "#layers/ui/app/lib/dates";
import { percentDone, progressFigures } from "#layers/ui/app/lib/progress";

const { $t, $ts } = useI18n();

/**
 * How much of a project, a cycle or a milestone is done, now and day by day.
 *
 * The figures come first and are always there. The chart is a picture of the
 * same numbers; when it cannot be drawn, the same numbers are shown as a table
 * instead, from the same rows, so the fallback says what the chart would have.
 *
 * Asked for again whenever `freshness` changes — a card moved on the board
 * beside it — and when the window comes back into focus: the server counts on
 * every request, so there is nothing to wait for but the request.
 */
const props = defineProps<{
  projectKey: string;
  of?: { cycle?: string; milestone?: string };
  /** Changed by the page whenever something it shows may have moved. */
  freshness?: number;
  /** Figures only: for a strip beside a board, where a chart would crowd it. */
  compact?: boolean;
}>();

const projectsApi = useProjects();
const progress = ref<Progress | null>(null);
const failed = ref<string | null>(null);

async function load() {
  try {
    progress.value = await projectsApi.progress(props.projectKey, props.of);
    failed.value = null;
  } catch (cause) {
    failed.value = isApiError(cause) ? cause.message : $ts("projects.couldNotLoad");
  }
}
onMounted(load);
watch(() => [props.projectKey, props.of?.cycle, props.of?.milestone, props.freshness], load);
const focused = useWindowFocus();
watch(focused, (now) => {
  if (now) void load();
});

const figures = computed(() =>
  progress.value
    ? progressFigures(progress.value, {
        completed: $ts("projects.progress.completed"),
        total: $ts("projects.progress.total"),
        describedAs: $ts("projects.progress.describedAs", {
          completed: progress.value.completed,
          total: progress.value.total,
          from: onCalendarDay(progress.value.from),
          to: onCalendarDay(progress.value.to),
        }),
      })
    : null,
);
</script>

<template>
  <section :aria-label="$ts('projects.progress.title')" class="space-y-2" data-progress>
    <p v-if="failed" class="text-sm text-destructive" role="alert">{{ failed }}</p>
    <template v-if="progress && figures">
      <div class="flex flex-wrap items-center gap-3" aria-live="polite">
        <PlanProgress :completed="progress.completed" :total="progress.total" />
        <span class="text-xs text-muted-foreground tabular-nums" data-percent>
          {{
            $t("projects.progress.percent", {
              percent: percentDone(progress.completed, progress.total),
            })
          }}
        </span>
      </div>
      <template v-if="!props.compact">
        <Chart :spec="figures.spec">
          <template #fallback>
            <table class="w-full max-w-md text-xs tabular-nums">
              <caption class="mb-1 text-left text-muted-foreground">
                {{
                  $t("projects.progress.table")
                }}
              </caption>
              <thead>
                <tr class="text-left text-muted-foreground">
                  <th scope="col" class="py-0.5 pr-3 font-medium">
                    {{ $t("projects.progress.day") }}
                  </th>
                  <th scope="col" class="py-0.5 pr-3 font-medium">
                    {{ $t("projects.progress.completed") }}
                  </th>
                  <th scope="col" class="py-0.5 font-medium">
                    {{ $t("projects.progress.total") }}
                  </th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="row in figures.rows" :key="row.day" class="border-t">
                  <th scope="row" class="py-0.5 pr-3 text-left font-normal">{{ row.label }}</th>
                  <td class="py-0.5 pr-3">{{ row.completed }}</td>
                  <td class="py-0.5">{{ row.total }}</td>
                </tr>
              </tbody>
            </table>
          </template>
        </Chart>
      </template>
    </template>
    <Skeleton v-else-if="!failed" class="h-6 w-48 rounded-md" />
  </section>
</template>
