<script setup lang="ts">
import { isApiError, type Cycle, type Issue, type Milestone, type PlanRef } from "@tdc/types";
import { Field } from "#layers/ui/app/components/ui/field";
import { Select } from "#layers/ui/app/components/ui/select";
import { onCalendarWeekday } from "#layers/ui/app/lib/dates";

const { $t, $ts, $localeRoute: localeRoute } = useI18n();

/**
 * Which cycle an issue is in and which milestone it counts towards.
 *
 * Shown on the issue's page and from a board card alike, and one change at a
 * time like every other field: each is its own recorded event. Somebody who
 * may not plan sees the names, linked, rather than the controls.
 */
const props = defineProps<{
  projectKey: string;
  issueKey: string;
  cycle?: PlanRef;
  milestone?: PlanRef;
  canPlan: boolean;
}>();
const emit = defineEmits<{ changed: [issue: Issue] }>();

const projectsApi = useProjects();
const cycles = ref<Cycle[]>([]);
const milestones = ref<Milestone[]>([]);
const error = ref<string | null>(null);

onMounted(async () => {
  if (!props.canPlan) return;
  try {
    [cycles.value, milestones.value] = await Promise.all([
      projectsApi.cycles(props.projectKey),
      projectsApi.milestones(props.projectKey),
    ]);
  } catch {
    // Without the lists there is nothing to offer; the names still show.
  }
});

const cycleOptions = computed(() => [
  { value: "none", label: $ts("projects.planning.noCycle") },
  ...cycles.value.map((cycle) => ({
    value: cycle.id,
    label: `${cycle.name} · ${onCalendarWeekday(cycle.startsOn)} – ${onCalendarWeekday(cycle.endsOn)}`,
  })),
]);
const milestoneOptions = computed(() => [
  { value: "none", label: $ts("projects.planning.noMilestone") },
  ...milestones.value.map((milestone) => ({ value: milestone.id, label: milestone.name })),
]);

async function change(work: () => Promise<Issue>) {
  error.value = null;
  try {
    emit("changed", await work());
  } catch (cause) {
    error.value = isApiError(cause) ? cause.message : $ts("projects.couldNotSave");
  }
}

function setCycle(value: string | undefined) {
  if (!value || value === (props.cycle?.id ?? "none")) return;
  void change(() => projectsApi.setCycle(props.issueKey, value === "none" ? null : value));
}
function setMilestone(value: string | undefined) {
  if (!value || value === (props.milestone?.id ?? "none")) return;
  void change(() => projectsApi.setMilestone(props.issueKey, value === "none" ? null : value));
}
</script>

<template>
  <div class="space-y-3">
    <template v-if="props.canPlan">
      <Field :label="$ts('projects.planning.cycle')">
        <template #default="{ id }">
          <Select
            :id="id"
            :model-value="props.cycle?.id ?? 'none'"
            :label="$ts('projects.planning.cycle')"
            :options="cycleOptions"
            @update:model-value="setCycle"
          />
        </template>
      </Field>
      <Field :label="$ts('projects.planning.milestone')">
        <template #default="{ id }">
          <Select
            :id="id"
            :model-value="props.milestone?.id ?? 'none'"
            :label="$ts('projects.planning.milestone')"
            :options="milestoneOptions"
            @update:model-value="setMilestone"
          />
        </template>
      </Field>
    </template>
    <dl v-else class="space-y-2 text-sm">
      <div>
        <dt class="text-xs font-semibold text-muted-foreground">
          {{ $t("projects.planning.cycle") }}
        </dt>
        <dd>
          <NuxtLink
            v-if="props.cycle"
            :to="
              localeRoute({
                name: 'projects-key-cycles-cycle',
                params: { key: props.projectKey, cycle: props.cycle.id },
              })
            "
            class="hover:underline"
          >
            {{ props.cycle.name }}
          </NuxtLink>
          <span v-else class="text-muted-foreground">{{ $t("projects.planning.noCycle") }}</span>
        </dd>
      </div>
      <div>
        <dt class="text-xs font-semibold text-muted-foreground">
          {{ $t("projects.planning.milestone") }}
        </dt>
        <dd>
          <NuxtLink
            v-if="props.milestone"
            :to="
              localeRoute({
                name: 'projects-key-milestones-milestone',
                params: { key: props.projectKey, milestone: props.milestone.id },
              })
            "
            class="hover:underline"
          >
            {{ props.milestone.name }}
          </NuxtLink>
          <span v-else class="text-muted-foreground">
            {{ $t("projects.planning.noMilestone") }}
          </span>
        </dd>
      </div>
    </dl>
    <p v-if="error" class="text-sm text-destructive" role="alert">{{ error }}</p>
  </div>
</template>
