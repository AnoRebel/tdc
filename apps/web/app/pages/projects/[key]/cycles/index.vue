<script setup lang="ts">
import { useForm } from "@tanstack/vue-form";
import type { AnyFieldApi } from "@tanstack/vue-form";
import * as v from "valibot";

import { isApiError, type Cycle, type Project } from "@tdc/types";
import { Button } from "#layers/ui/app/components/ui/button";
import { Field } from "#layers/ui/app/components/ui/field";
import { Input } from "#layers/ui/app/components/ui/input";
import { daysAfter, onCalendarDay, todayIn } from "#layers/ui/app/lib/dates";

const { $t, $ts, $localeRoute: localeRoute } = useI18n();

/**
 * A project's cycles: time-boxes, in the project's own calendar.
 *
 * Optional — a project that never makes one loses nothing — and the dates are
 * days, shown exactly as they were chosen, with the zone they are in said
 * once at the top rather than converted into the reader's.
 */

definePageMeta({ middleware: "auth", layout: "app" });

const route = useRoute();
const key = computed(() => String(route.params.key).toUpperCase());
const projectsApi = useProjects();

const project = ref<Project | null>(null);
const cycles = ref<Cycle[]>([]);
const missing = ref(false);
const failed = ref<string | null>(null);
const making = ref(false);

useHead({
  title: computed(() => `${project.value?.name ?? key.value} · ${$ts("projects.tabs.cycles")}`),
});

async function load() {
  failed.value = null;
  try {
    [project.value, cycles.value] = await Promise.all([
      projectsApi.get(key.value),
      projectsApi.cycles(key.value),
    ]);
  } catch (cause) {
    if (isApiError(cause) && cause.status === 404) missing.value = true;
    else failed.value = isApiError(cause) ? cause.message : $ts("projects.couldNotLoad");
  }
}
onMounted(load);

const day = v.pipe(v.string(), v.regex(/^\d{4}-\d{2}-\d{2}$/, $ts("projects.planning.dateFormat")));
const schema = v.pipe(
  v.object({
    name: v.pipe(
      v.string(),
      v.trim(),
      v.minLength(1, $ts("projects.planning.nameRequired")),
      v.maxLength(60, $ts("projects.planning.nameTooLong")),
    ),
    startsOn: day,
    endsOn: day,
  }),
  // Days written YYYY-MM-DD compare as text the way they compare as days.
  v.forward(
    v.check((value) => value.endsOn >= value.startsOn, $ts("projects.planning.endsBeforeStart")),
    ["endsOn"],
  ),
);

const refused = ref<string | null>(null);
const form = useForm({
  defaultValues: { name: "", startsOn: "", endsOn: "" },
  validators: { onSubmit: schema },
  async onSubmit({ value }) {
    refused.value = null;
    try {
      await projectsApi.createCycle(key.value, {
        name: value.name.trim(),
        startsOn: value.startsOn,
        endsOn: value.endsOn,
      });
      making.value = false;
      await load();
    } catch (cause) {
      refused.value = isApiError(cause) ? cause.message : $ts("projects.couldNotSave");
    }
  },
});

/** Two weeks from today, in the project's calendar. */
function startMaking() {
  const today = todayIn(project.value?.timeZone ?? "UTC");
  form.reset();
  form.setFieldValue("startsOn", today);
  form.setFieldValue("endsOn", daysAfter(today, 13));
  making.value = true;
}

function firstError(field: AnyFieldApi): string | undefined {
  if (!field.state.meta.isTouched && !form.state.isSubmitted) return undefined;
  const [issue] = field.state.meta.errors;
  if (!issue) return undefined;
  return typeof issue === "string" ? issue : (issue as { message?: string }).message;
}

const fields = [
  { name: "name" as const, label: "projects.planning.name", type: "text" },
  { name: "startsOn" as const, label: "projects.planning.startsOn", type: "date" },
  { name: "endsOn" as const, label: "projects.planning.endsOn", type: "date" },
];
</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col">
    <div v-if="missing" class="grid flex-1 place-items-center p-6 text-sm">
      {{ $t("projects.notFound") }}
    </div>
    <p v-else-if="failed" class="p-4 text-sm text-destructive" role="alert">{{ failed }}</p>
    <template v-else-if="project">
      <ProjectsProjectHeader :project="project" current="cycles">
        <template #actions>
          <Button v-if="project.can?.plan && !making" class="ml-auto" @click="startMaking">
            <Icon name="lucide:plus" class="size-3.5" />
            {{ $t("projects.planning.newCycle") }}
          </Button>
        </template>
      </ProjectsProjectHeader>

      <div class="flex-1 space-y-4 overflow-y-auto px-4 py-4">
        <p class="text-xs text-muted-foreground">
          {{
            $t("projects.planning.calendar", {
              zone: project.timeZone,
              policy: $ts(`projects.planning.policies.${project.cyclePolicy}`),
            })
          }}
        </p>

        <section
          v-if="making"
          class="max-w-xl rounded-xl border bg-card p-4"
          :aria-label="$ts('projects.planning.newCycle')"
        >
          <form class="space-y-3" @submit.prevent="form.handleSubmit()">
            <form.Field v-for="entry in fields" :key="entry.name" :name="entry.name">
              <template #default="{ field }">
                <Field :label="$ts(entry.label)" :error="firstError(field)" required>
                  <template #default="{ id, invalid, describedBy }">
                    <Input
                      :id="id"
                      :type="entry.type"
                      :model-value="field.state.value"
                      :invalid="invalid"
                      :described-by="describedBy"
                      @update:model-value="field.handleChange(String($event))"
                      @blur="field.handleBlur"
                    />
                  </template>
                </Field>
              </template>
            </form.Field>
            <p v-if="refused" class="text-sm text-destructive" role="alert">{{ refused }}</p>
            <div class="flex justify-end gap-2">
              <Button variant="ghost" @click="making = false">{{ $t("action.cancel") }}</Button>
              <Button type="submit">{{ $t("projects.planning.makeCycle") }}</Button>
            </div>
          </form>
        </section>

        <p v-if="!cycles.length" class="text-sm text-muted-foreground">
          {{ $t("projects.planning.noCycles") }}
        </p>
        <ul v-else class="max-w-3xl divide-y rounded-lg border bg-surface">
          <li
            v-for="cycle in cycles"
            :key="cycle.id"
            class="flex flex-wrap items-center gap-3 px-3 py-2"
          >
            <NuxtLink
              :to="
                localeRoute({
                  name: 'projects-key-cycles-cycle',
                  params: { key: project.key, cycle: cycle.id },
                })
              "
              class="min-w-0 flex-1 truncate text-sm font-medium hover:underline"
            >
              {{ cycle.name }}
            </NuxtLink>
            <span class="text-xs text-muted-foreground tabular-nums">
              {{ onCalendarDay(cycle.startsOn) }} – {{ onCalendarDay(cycle.endsOn) }}
            </span>
            <span
              class="rounded border px-1.5 py-0.5 text-[0.65rem] font-medium"
              :data-status="cycle.status"
            >
              {{ $t(`projects.planning.status.${cycle.status}`) }}
            </span>
            <ProjectsPlanProgress :completed="cycle.completed" :total="cycle.total" />
          </li>
        </ul>
      </div>
    </template>
    <div v-else class="space-y-2 p-4" aria-busy="true">
      <UiSkeleton v-for="n in 3" :key="n" class="h-9 w-full rounded-md" />
    </div>
  </div>
</template>
