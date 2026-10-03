<script setup lang="ts">
import { useForm } from "@tanstack/vue-form";
import type { AnyFieldApi } from "@tanstack/vue-form";
import * as v from "valibot";

import { isApiError, type Milestone, type Project } from "@tdc/types";
import { Button } from "#layers/ui/app/components/ui/button";
import { Field } from "#layers/ui/app/components/ui/field";
import { Input } from "#layers/ui/app/components/ui/input";
import { onCalendarDay } from "#layers/ui/app/lib/dates";

const { $t, $ts, $localeRoute: localeRoute } = useI18n();

/**
 * A project's milestones: issues gathered across cycles, with a day to aim for.
 *
 * The day is advisory. Passing it with work unfinished marks the milestone
 * overdue, plainly, and changes nothing about any issue in it.
 */

definePageMeta({ middleware: "auth", layout: "app" });

const route = useRoute();
const key = computed(() => String(route.params.key).toUpperCase());
const projectsApi = useProjects();

const project = ref<Project | null>(null);
const milestones = ref<Milestone[]>([]);
const missing = ref(false);
const failed = ref<string | null>(null);
const making = ref(false);

useHead({
  title: computed(() => `${project.value?.name ?? key.value} · ${$ts("projects.tabs.milestones")}`),
});

async function load() {
  failed.value = null;
  try {
    [project.value, milestones.value] = await Promise.all([
      projectsApi.get(key.value),
      projectsApi.milestones(key.value),
    ]);
  } catch (cause) {
    if (isApiError(cause) && cause.status === 404) missing.value = true;
    else failed.value = isApiError(cause) ? cause.message : $ts("projects.couldNotLoad");
  }
}
onMounted(load);

const schema = v.object({
  name: v.pipe(
    v.string(),
    v.trim(),
    v.minLength(1, $ts("projects.planning.nameRequired")),
    v.maxLength(80, $ts("projects.planning.nameTooLong")),
  ),
  targetDate: v.union([
    v.literal(""),
    v.pipe(v.string(), v.regex(/^\d{4}-\d{2}-\d{2}$/, $ts("projects.planning.dateFormat"))),
  ]),
  description: v.pipe(v.string(), v.maxLength(2000)),
});

const refused = ref<string | null>(null);
const form = useForm({
  defaultValues: { name: "", targetDate: "", description: "" },
  validators: { onSubmit: schema },
  async onSubmit({ value }) {
    refused.value = null;
    try {
      await projectsApi.createMilestone(key.value, {
        name: value.name.trim(),
        targetDate: value.targetDate,
        description: value.description.trim(),
      });
      making.value = false;
      await load();
    } catch (cause) {
      refused.value = isApiError(cause) ? cause.message : $ts("projects.couldNotSave");
    }
  },
});

function firstError(field: AnyFieldApi): string | undefined {
  if (!field.state.meta.isTouched && !form.state.isSubmitted) return undefined;
  const [issue] = field.state.meta.errors;
  if (!issue) return undefined;
  return typeof issue === "string" ? issue : (issue as { message?: string }).message;
}

const fields = [
  { name: "name" as const, label: "projects.planning.name", type: "text", required: true },
  {
    name: "targetDate" as const,
    label: "projects.planning.targetDate",
    type: "date",
    required: false,
  },
  { name: "description" as const, label: "projects.description", type: "text", required: false },
];
</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col">
    <div v-if="missing" class="grid flex-1 place-items-center p-6 text-sm">
      {{ $t("projects.notFound") }}
    </div>
    <p v-else-if="failed" class="p-4 text-sm text-destructive" role="alert">{{ failed }}</p>
    <template v-else-if="project">
      <ProjectsProjectHeader :project="project" current="milestones">
        <template #actions>
          <Button
            v-if="project.can?.plan && !making"
            class="ml-auto"
            @click="((making = true), form.reset())"
          >
            <Icon name="lucide:plus" class="size-3.5" />
            {{ $t("projects.planning.newMilestone") }}
          </Button>
        </template>
      </ProjectsProjectHeader>

      <div class="flex-1 space-y-4 overflow-y-auto px-4 py-4">
        <section
          v-if="making"
          class="max-w-xl rounded-xl border bg-card p-4"
          :aria-label="$ts('projects.planning.newMilestone')"
        >
          <form class="space-y-3" @submit.prevent="form.handleSubmit()">
            <form.Field v-for="entry in fields" :key="entry.name" :name="entry.name">
              <template #default="{ field }">
                <Field
                  :label="$ts(entry.label)"
                  :error="firstError(field)"
                  :required="entry.required"
                >
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
              <Button type="submit">{{ $t("projects.planning.makeMilestone") }}</Button>
            </div>
          </form>
        </section>

        <p v-if="!milestones.length" class="text-sm text-muted-foreground">
          {{ $t("projects.planning.noMilestones") }}
        </p>
        <ul v-else class="max-w-3xl divide-y rounded-lg border bg-surface">
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
            <span v-if="milestone.targetDate" class="text-xs text-muted-foreground tabular-nums">
              {{ $t("projects.planning.target", { day: onCalendarDay(milestone.targetDate) }) }}
            </span>
            <ProjectsOverdueBadge v-if="milestone.overdue" />
            <ProjectsPlanProgress :completed="milestone.completed" :total="milestone.total" />
          </li>
        </ul>
      </div>
    </template>
    <div v-else class="space-y-2 p-4" aria-busy="true">
      <UiSkeleton v-for="n in 3" :key="n" class="h-9 w-full rounded-md" />
    </div>
  </div>
</template>
