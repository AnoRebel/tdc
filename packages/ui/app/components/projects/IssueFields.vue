<script setup lang="ts">
import { nameOf } from "#layers/ui/app/lib/names";
import {
  isApiError,
  type Issue,
  type ProjectMember,
  type RelatedIssue,
  type RelationKind,
} from "@tdc/types";
import { useForm } from "@tanstack/vue-form";
import * as v from "valibot";

import { Button } from "#layers/ui/app/components/ui/button";
import { Field } from "#layers/ui/app/components/ui/field";
import { Input } from "#layers/ui/app/components/ui/input";
import { Select } from "#layers/ui/app/components/ui/select";
import LabelChip from "#layers/ui/app/components/projects/LabelChip.vue";
import PriorityBadge from "#layers/ui/app/components/projects/PriorityBadge.vue";
import IssuePlanning from "#layers/ui/app/components/projects/IssuePlanning.vue";

const { $t, $ts, $localeRoute: localeRoute } = useI18n();

/**
 * Who is on an issue, how it is classified, and how it relates to others.
 *
 * Each control makes one change the moment it is used — assign, label,
 * prioritise, relate — because each is its own recorded event, and the issue
 * comes back from the server as it now is. Somebody without the permission for
 * a field sees it as a value rather than a control.
 */
const props = defineProps<{ issue: Issue }>();
const emit = defineEmits<{ changed: [issue: Issue] }>();

const projectsApi = useProjects();
const error = ref<string | null>(null);
const members = ref<ProjectMember[]>([]);

const project = computed(() => props.issue.project);
const can = computed(() => props.issue.can);

onMounted(async () => {
  if (!can.value?.assign || !project.value) return;
  try {
    members.value = await projectsApi.members(project.value.key);
  } catch {
    // Without the list there is nobody to offer; the rest still works.
  }
});

/** Runs a change and shows the issue as the server now has it. */
async function change(work: () => Promise<Issue>) {
  error.value = null;
  try {
    emit("changed", await work());
  } catch (cause) {
    error.value = isApiError(cause) ? cause.message : $ts("projects.couldNotSave");
  }
}

const assignable = computed(() =>
  members.value
    // Somebody on another server cannot be assigned until their server can
    // act on the project for them.
    .filter((member) => !member.remote)
    .filter((member) => !props.issue.assignees.some((a) => a.username === member.username))
    .map((member) => ({
      value: member.username,
      label: nameOf(member),
    })),
);

function assign(username: string | undefined) {
  if (username) void change(() => projectsApi.assign(props.issue.key, username));
}

const labels = computed(() => project.value?.labels ?? []);
function hasLabel(id: string) {
  return props.issue.labels.some((label) => label.id === id);
}
function toggleLabel(id: string) {
  void change(() =>
    hasLabel(id)
      ? projectsApi.unlabel(props.issue.key, id)
      : projectsApi.label(props.issue.key, id),
  );
}

const priorityOptions = computed(() => [
  { value: "none", label: $ts("projects.priority.noneOption") },
  ...(project.value?.priorities ?? []).map((priority) => ({
    value: priority.id,
    label: priority.name,
  })),
]);
function prioritise(value: string | undefined) {
  if (!value) return;
  void change(() => projectsApi.prioritise(props.issue.key, value === "none" ? null : value));
}

/* Relationships. */
const relationKinds = computed(() => [
  { value: "blocks" as RelationKind, label: $ts("projects.relation.addBlocks") },
  { value: "blocked_by" as RelationKind, label: $ts("projects.relation.addBlockedBy") },
  { value: "parent" as RelationKind, label: $ts("projects.relation.addParentOf") },
  { value: "child" as RelationKind, label: $ts("projects.relation.addChildOf") },
]);
/*
 * The other issue is named by its identifier, and only its shape is checked
 * here: whether it exists, is in this project, and would close a loop are the
 * server's to say — and it says which issues the loop would run through.
 */
const relationForm = useForm({
  defaultValues: { kind: "blocks" as RelationKind, other: "" },
  validators: {
    onSubmit: v.object({
      kind: v.picklist(["parent", "child", "blocks", "blocked_by"]),
      other: v.pipe(v.string(), v.trim(), v.regex(/^[A-Za-z][A-Za-z0-9]{1,9}-[0-9]+$/)),
    }),
  },
  async onSubmit({ value }) {
    await change(() =>
      projectsApi.relate(props.issue.key, value.kind, value.other.trim().toUpperCase()),
    );
    if (!error.value) relationForm.setFieldValue("other", "");
  },
});

const relations = computed(() => props.issue.relations);
const groups = computed(() => {
  const found = relations.value;
  if (!found) return [];
  return [
    {
      kind: "child" as RelationKind,
      title: $ts("projects.relation.parentTitle"),
      issues: found.parent ? [found.parent] : [],
    },
    {
      kind: "parent" as RelationKind,
      title: $ts("projects.relation.childrenTitle"),
      issues: found.children,
    },
    {
      kind: "blocks" as RelationKind,
      title: $ts("projects.relation.blocksTitle"),
      issues: found.blocks,
    },
    {
      kind: "blocked_by" as RelationKind,
      title: $ts("projects.relation.blockedByTitle"),
      issues: found.blockedBy,
    },
  ].filter((group) => group.issues.length);
});

function unrelate(kind: RelationKind, other: RelatedIssue) {
  void change(() => projectsApi.unrelate(props.issue.key, kind, other.key));
}
</script>

<template>
  <div class="space-y-4 text-sm">
    <p
      v-if="error"
      class="rounded-md bg-destructive/10 px-2 py-1.5 text-xs text-destructive"
      role="alert"
    >
      {{ error }}
    </p>

    <section :aria-label="$ts('projects.assignees')">
      <h2 class="mb-1 text-xs font-semibold text-muted-foreground">
        {{ $t("projects.assignees") }}
      </h2>
      <ul v-if="props.issue.assignees.length" class="space-y-1">
        <li
          v-for="person in props.issue.assignees"
          :key="person.username"
          class="flex items-center gap-2"
        >
          <UiAvatar :name="nameOf(person)" size="sm" />
          <span class="min-w-0 flex-1 truncate text-xs">{{ nameOf(person) }}</span>
          <Button
            v-if="can?.assign"
            variant="ghost"
            size="icon"
            class="size-6"
            :label="$ts('projects.unassign', { who: nameOf(person) })"
            @click="change(() => projectsApi.unassign(props.issue.key, person.username))"
          >
            <Icon name="lucide:x" class="size-3" />
          </Button>
        </li>
      </ul>
      <p v-else class="text-xs text-muted-foreground">{{ $t("projects.nobodyAssigned") }}</p>
      <Select
        v-if="can?.assign && assignable.length"
        class="mt-1.5"
        :model-value="undefined"
        :placeholder="$ts('projects.assignSomebody')"
        :label="$ts('projects.assignSomebody')"
        :options="assignable"
        @update:model-value="assign"
      />
    </section>

    <section :aria-label="$ts('projects.labels')">
      <h2 class="mb-1 text-xs font-semibold text-muted-foreground">{{ $t("projects.labels") }}</h2>
      <div v-if="can?.label && labels.length" class="flex flex-wrap gap-1">
        <button
          v-for="label in labels"
          :key="label.id"
          type="button"
          class="rounded-full transition-opacity"
          :class="hasLabel(label.id) ? 'opacity-100' : 'opacity-55 hover:opacity-90'"
          :aria-pressed="hasLabel(label.id)"
          @click="toggleLabel(label.id)"
        >
          <LabelChip :label="label" />
        </button>
      </div>
      <div v-else-if="props.issue.labels.length" class="flex flex-wrap gap-1">
        <LabelChip v-for="label in props.issue.labels" :key="label.id" :label="label" />
      </div>
      <p v-else class="text-xs text-muted-foreground">{{ $t("projects.noLabels") }}</p>
    </section>

    <Field v-if="can?.label" :label="$ts('projects.priority.title')">
      <template #default="{ id }">
        <Select
          :id="id"
          :model-value="props.issue.priority?.id ?? 'none'"
          :label="$ts('projects.priority.title')"
          :options="priorityOptions"
          @update:model-value="prioritise"
        />
      </template>
    </Field>
    <section v-else :aria-label="$ts('projects.priority.title')">
      <h2 class="mb-1 text-xs font-semibold text-muted-foreground">
        {{ $t("projects.priority.title") }}
      </h2>
      <PriorityBadge :priority="props.issue.priority" />
    </section>

    <IssuePlanning
      v-if="project"
      :project-key="project.key"
      :issue-key="props.issue.key"
      :cycle="props.issue.cycle"
      :milestone="props.issue.milestone"
      :can-plan="Boolean(can?.plan)"
      @changed="(updated) => emit('changed', updated)"
    />

    <section :aria-label="$ts('projects.relation.title')">
      <h2 class="mb-1 text-xs font-semibold text-muted-foreground">
        {{ $t("projects.relation.title") }}
      </h2>

      <div v-if="relations && relations.childProgress.total" class="mb-2">
        <p class="text-xs">
          {{
            $t("projects.relation.childProgress", {
              completed: relations.childProgress.completed,
              total: relations.childProgress.total,
            })
          }}
        </p>
        <div
          class="mt-1 h-1.5 overflow-hidden rounded-full bg-muted"
          role="progressbar"
          :aria-valuenow="relations.childProgress.completed"
          aria-valuemin="0"
          :aria-valuemax="relations.childProgress.total"
          :aria-label="$ts('projects.relation.childrenTitle')"
        >
          <div
            class="h-full bg-success"
            :style="{
              width: `${(100 * relations.childProgress.completed) / relations.childProgress.total}%`,
            }"
          />
        </div>
      </div>

      <div v-for="group in groups" :key="group.kind" class="mb-2">
        <h3 class="text-[0.6875rem] text-muted-foreground">{{ group.title }}</h3>
        <ul class="mt-0.5 space-y-0.5">
          <li
            v-for="other in group.issues"
            :key="other.key"
            class="flex items-center gap-1.5 text-xs"
          >
            <Icon
              v-if="other.completed"
              name="lucide:circle-check"
              class="size-3 shrink-0 text-success-text"
              aria-hidden="true"
            />
            <Icon
              v-else
              name="lucide:circle-dashed"
              class="size-3 shrink-0 text-muted-foreground"
              aria-hidden="true"
            />
            <NuxtLink
              :to="localeRoute({ name: 'issues-ref', params: { ref: other.key } })"
              class="min-w-0 flex-1 truncate hover:underline"
            >
              <span class="font-mono font-semibold">{{ other.key }}</span>
              {{ other.title }}
            </NuxtLink>
            <Button
              v-if="can?.relate"
              variant="ghost"
              size="icon"
              class="size-6"
              :label="$ts('projects.relation.remove', { other: other.key })"
              @click="unrelate(group.kind, other)"
            >
              <Icon name="lucide:x" class="size-3" />
            </Button>
          </li>
        </ul>
      </div>

      <form v-if="can?.relate" class="space-y-1.5" @submit.prevent="relationForm.handleSubmit()">
        <relationForm.Field name="kind">
          <template #default="{ field }">
            <Select
              :model-value="field.state.value"
              :label="$ts('projects.relation.kind')"
              :options="relationKinds"
              @update:model-value="$event && field.handleChange($event)"
            />
          </template>
        </relationForm.Field>
        <relationForm.Field name="other">
          <template #default="{ field }">
            <div class="flex gap-1.5">
              <Input
                :model-value="field.state.value"
                class="h-8 font-mono uppercase"
                :label="$ts('projects.relation.otherIssue')"
                :placeholder="`${project?.key ?? 'WEB'}-1`"
                @update:model-value="field.handleChange($event)"
              />
              <Button type="submit" size="small" class="h-8" :disabled="!field.state.value.trim()">
                {{ $t("projects.relation.add") }}
              </Button>
            </div>
          </template>
        </relationForm.Field>
      </form>
    </section>
  </div>
</template>
