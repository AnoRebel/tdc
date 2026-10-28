<script setup lang="ts">
import { nameOf } from "#layers/ui/app/lib/names";
import { useForm } from "@tanstack/vue-form";
import * as v from "valibot";

import {
  isApiError,
  type CyclePolicy,
  type IssueState,
  type Project,
  type ProjectMember,
} from "@tdc/types";
import { Button } from "#layers/ui/app/components/ui/button";
import { Checkbox } from "#layers/ui/app/components/ui/checkbox";
import { Field } from "#layers/ui/app/components/ui/field";
import { Input } from "#layers/ui/app/components/ui/input";
import { Select } from "#layers/ui/app/components/ui/select";
import { Switch } from "#layers/ui/app/components/ui/switch";

const { $t, $ts } = useI18n();

/**
 * How a project works: its states, who is in it, and who can see it.
 *
 * Each change is made as it is made — no page-wide save — because each is
 * its own decision, recorded in the project's history as its own event.
 * Somebody without the permission for a section sees it read-only rather
 * than a form that would be refused.
 */

definePageMeta({ middleware: "auth", layout: "app" });

const route = useRoute();
const key = computed(() => String(route.params.key).toUpperCase());
const projectsApi = useProjects();
const { offers } = useServerInfo();

const project = ref<Project | null>(null);
const members = ref<ProjectMember[]>([]);
const missing = ref(false);
const error = ref<string | null>(null);
const notice = ref<string | null>(null);

useHead({
  title: computed(() => `${project.value?.name ?? key.value} · ${$ts("projects.tabs.settings")}`),
});

async function load() {
  try {
    project.value = await projectsApi.get(key.value);
    members.value = await projectsApi.members(key.value);
  } catch (cause) {
    if (isApiError(cause) && cause.status === 404) missing.value = true;
    else error.value = isApiError(cause) ? cause.message : $ts("projects.couldNotLoad");
  }
}
onMounted(load);

/** Runs a change, reporting the server's refusal in its own words. */
async function attempt(change: () => Promise<unknown>, done?: string) {
  error.value = null;
  notice.value = null;
  try {
    await change();
    if (done) notice.value = done;
    await load();
  } catch (cause) {
    error.value = isApiError(cause) ? cause.message : $ts("projects.couldNotSave");
  }
}

const states = computed<IssueState[]>(() => project.value?.states ?? []);
const canConfigure = computed(() => Boolean(project.value?.can?.configureStates));
const canAdminister = computed(() => Boolean(project.value?.can?.administer));

/* Adding a state. */
const newState = ref("");
const newStateDone = ref(false);
async function addState() {
  const name = newState.value.trim();
  if (!name) return;
  await attempt(() => projectsApi.addState(key.value, { name, isCompletion: newStateDone.value }));
  newState.value = "";
  newStateDone.value = false;
}

/* Renaming, in place. */
const renaming = ref<string | null>(null);
const renameTo = ref("");
function startRename(state: IssueState) {
  renaming.value = state.id;
  renameTo.value = state.name;
}
async function rename(state: IssueState) {
  const name = renameTo.value.trim();
  renaming.value = null;
  if (!name || name === state.name) return;
  await attempt(() => projectsApi.updateState(key.value, state.id, { name }));
}

/*
 * Reordering by moving one step at a time, with buttons rather than dragging
 * alone, so it can be done from a keyboard and read by a screen reader.
 */
async function shift(index: number, by: -1 | 1) {
  const order = states.value.map((state) => state.id);
  const target = index + by;
  if (target < 0 || target >= order.length) return;
  [order[index], order[target]] = [order[target]!, order[index]!];
  await attempt(() => projectsApi.reorderStates(key.value, order));
}

/*
 * Removing a state asks where its issues go. The server refuses to guess, and
 * so does this: moving somebody's work into a column they did not choose is
 * not something to do silently.
 */
const removing = ref<IssueState | null>(null);
const moveTo = ref<string>("");
function askToRemove(state: IssueState) {
  removing.value = state;
  moveTo.value = states.value.find((other) => other.id !== state.id)?.id ?? "";
}
async function remove() {
  const state = removing.value;
  removing.value = null;
  if (!state) return;
  await attempt(
    () => projectsApi.removeState(key.value, state.id, moveTo.value || undefined),
    $ts("projects.stateRemoved", { state: state.name }),
  );
}
const destinations = computed(() =>
  states.value
    .filter((state) => state.id !== removing.value?.id)
    .map((state) => ({ value: state.id, label: state.name })),
);

/*
 * Labels: a name and a colour from a short palette. The palette rather than
 * any colour, so labels stay distinguishable from each other; the name is
 * always written beside the dot, so the colour is never the only signal.
 */
const palette = [
  { value: "#6b7280", name: "grey" },
  { value: "#dc2626", name: "red" },
  { value: "#ea580c", name: "orange" },
  { value: "#ca8a04", name: "yellow" },
  { value: "#16a34a", name: "green" },
  { value: "#0891b2", name: "teal" },
  { value: "#2563eb", name: "blue" },
  { value: "#7c3aed", name: "violet" },
  { value: "#db2777", name: "pink" },
] as const;
const canLabel = computed(() => Boolean(project.value?.can?.label));
const newLabel = ref("");
const newLabelColour = ref<string>(palette[1].value);
async function addLabel() {
  const name = newLabel.value.trim();
  if (!name) return;
  await attempt(() => projectsApi.addLabel(key.value, { name, colour: newLabelColour.value }));
  if (!error.value) newLabel.value = "";
}

/* The priority scale, most urgent first, reordered one step at a time. */
const priorities = computed(() => project.value?.priorities ?? []);
const newPriority = ref("");
async function addPriority() {
  const name = newPriority.value.trim();
  if (!name) return;
  await attempt(() => projectsApi.addPriority(key.value, name));
  if (!error.value) newPriority.value = "";
}
async function shiftPriority(index: number, by: -1 | 1) {
  const order = priorities.value.map((priority) => priority.id);
  const target = index + by;
  if (target < 0 || target >= order.length) return;
  [order[index], order[target]] = [order[target]!, order[index]!];
  await attempt(() => projectsApi.reorderPriorities(key.value, order));
}

/* Members: somebody named, and what they may do here. */
const roles = computed(() => [
  { value: "member" as const, label: $ts("projects.roleMember") },
  { value: "admin" as const, label: $ts("projects.roleAdmin") },
]);
const memberForm = useForm({
  defaultValues: { username: "", role: "member" as "admin" | "member" },
  validators: {
    onSubmit: v.object({
      username: v.pipe(v.string(), v.trim(), v.minLength(1, $ts("projects.usernameRequired"))),
      role: v.picklist(["admin", "member"]),
    }),
  },
  async onSubmit({ value }) {
    await attempt(() => projectsApi.setMember(key.value, value.username.trim(), value.role));
    if (!error.value) memberForm.reset();
  },
});

/*
 * Sharing with other servers. Shown where this server federates, and on a
 * project already opened even where it no longer does, so it can be closed.
 */
const sharable = computed(() => offers("projects.federated") || Boolean(project.value?.federated));
const federated = computed({
  get: () => Boolean(project.value?.federated),
  set: (value: boolean) =>
    void attempt(
      () => projectsApi.setFederated(key.value, value),
      value ? $ts("projects.federation.opened") : $ts("projects.federation.closed"),
    ),
});

/* Who can see it. */
const visibilities = computed(() => [
  { value: "public" as const, label: $ts("projects.public") },
  { value: "private" as const, label: $ts("projects.private") },
]);
async function setVisibility(visibility: "public" | "private" | undefined) {
  if (!visibility || visibility === project.value?.visibility) return;
  await attempt(() => projectsApi.update(key.value, { visibility }));
}

/*
 * The calendar its dates are on, and what a cycle's end does with unfinished
 * work. The zone is typed as its IANA name and checked by the server, which
 * knows every zone it can keep a calendar in.
 */
const zone = ref("");
watch(
  () => project.value?.timeZone,
  (current) => {
    if (current) zone.value = current;
  },
  { immediate: true },
);
async function saveZone() {
  const timeZone = zone.value.trim();
  if (!timeZone || timeZone === project.value?.timeZone) return;
  await attempt(
    () => projectsApi.setPlanning(key.value, { timeZone }),
    $ts("projects.planning.saved"),
  );
}
const policies = computed(() => [
  { value: "carry-forward" as const, label: $ts("projects.planning.policies.carry-forward") },
  { value: "remain" as const, label: $ts("projects.planning.policies.remain") },
]);
async function setPolicy(cyclePolicy: CyclePolicy | undefined) {
  if (!cyclePolicy || cyclePolicy === project.value?.cyclePolicy) return;
  await attempt(
    () => projectsApi.setPlanning(key.value, { cyclePolicy }),
    $ts("projects.planning.saved"),
  );
}
</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col">
    <div v-if="missing" class="grid flex-1 place-items-center p-6 text-sm">
      {{ $t("projects.notFound") }}
    </div>

    <template v-else-if="project">
      <ProjectsProjectHeader :project="project" current="settings" />

      <div class="flex-1 overflow-y-auto">
        <div class="max-w-2xl space-y-8 px-4 py-6">
          <p
            v-if="error"
            class="rounded-md bg-destructive/10 px-3 py-2 text-sm text-destructive"
            role="alert"
          >
            {{ error }}
          </p>
          <p v-if="notice" class="rounded-md bg-success/10 px-3 py-2 text-sm" role="status">
            {{ notice }}
          </p>

          <section :aria-labelledby="'states-heading'">
            <h2 id="states-heading" class="text-sm font-semibold">{{ $t("projects.states") }}</h2>
            <p class="mt-0.5 text-xs text-muted-foreground">{{ $t("projects.statesHint") }}</p>

            <ol class="mt-3 space-y-1">
              <li
                v-for="(state, index) in states"
                :key="state.id"
                class="flex items-center gap-2 rounded-lg border bg-card px-3 py-2"
              >
                <span class="tabular w-4 text-xs text-muted-foreground">{{ index + 1 }}</span>
                <template v-if="renaming === state.id">
                  <Input
                    v-model="renameTo"
                    class="h-7 flex-1"
                    :label="$ts('projects.stateName')"
                    @keydown.enter.prevent="rename(state)"
                    @keydown.esc="renaming = null"
                  />
                  <Button size="small" @click="rename(state)">{{ $t("action.save") }}</Button>
                </template>
                <template v-else>
                  <ProjectsStateBadge :state="state" class="flex-1" />
                  <template v-if="canConfigure">
                    <span class="flex items-center gap-1.5 text-xs text-muted-foreground">
                      <Checkbox
                        :id="`done-${state.id}`"
                        :model-value="state.isCompletion"
                        @update:model-value="
                          (value: boolean) =>
                            attempt(() =>
                              projectsApi.updateState(key, state.id, { isCompletion: value }),
                            )
                        "
                      />
                      <label :for="`done-${state.id}`">
                        <span class="sr-only">{{ state.name }}: </span
                        >{{ $t("projects.meansDone") }}
                      </label>
                    </span>
                    <Button
                      variant="ghost"
                      size="icon"
                      :label="$ts('projects.moveStateEarlier', { state: state.name })"
                      :disabled="index === 0"
                      @click="shift(index, -1)"
                    >
                      <Icon name="lucide:arrow-up" class="size-3.5" />
                    </Button>
                    <Button
                      variant="ghost"
                      size="icon"
                      :label="$ts('projects.moveStateLater', { state: state.name })"
                      :disabled="index === states.length - 1"
                      @click="shift(index, 1)"
                    >
                      <Icon name="lucide:arrow-down" class="size-3.5" />
                    </Button>
                    <Button
                      variant="ghost"
                      size="icon"
                      :label="$ts('projects.renameState', { state: state.name })"
                      @click="startRename(state)"
                    >
                      <Icon name="lucide:pencil" class="size-3.5" />
                    </Button>
                    <Button
                      variant="ghost"
                      size="icon"
                      :label="$ts('projects.removeState', { state: state.name })"
                      :disabled="states.length <= 1"
                      @click="askToRemove(state)"
                    >
                      <Icon name="lucide:trash-2" class="size-3.5" />
                    </Button>
                  </template>
                </template>
              </li>
            </ol>

            <form
              v-if="canConfigure"
              class="mt-3 flex flex-wrap items-center gap-2"
              @submit.prevent="addState"
            >
              <Field :label="$ts('projects.newState')" class="min-w-40 flex-1">
                <template #default="{ id }">
                  <Input :id="id" v-model="newState" />
                </template>
              </Field>
              <span class="flex items-center gap-1.5 self-end pb-2 text-xs text-muted-foreground">
                <Checkbox id="new-state-done" v-model="newStateDone" />
                <label for="new-state-done">{{ $t("projects.meansDone") }}</label>
              </span>
              <Button type="submit" class="self-end" :disabled="!newState.trim()">
                {{ $t("projects.addState") }}
              </Button>
            </form>
          </section>

          <section :aria-labelledby="'labels-heading'">
            <h2 id="labels-heading" class="text-sm font-semibold">{{ $t("projects.labels") }}</h2>
            <ul v-if="project.labels?.length" class="mt-3 flex flex-wrap gap-2">
              <li
                v-for="label in project.labels"
                :key="label.id"
                class="flex items-center gap-1 rounded-full border bg-card py-0.5 pr-0.5 pl-1"
              >
                <ProjectsLabelChip :label="label" class="border-0" />
                <Button
                  v-if="canLabel"
                  variant="ghost"
                  size="icon"
                  class="size-5"
                  :label="$ts('projects.removeLabel', { label: label.name })"
                  @click="attempt(() => projectsApi.removeLabel(key, label.id))"
                >
                  <Icon name="lucide:x" class="size-3" />
                </Button>
              </li>
            </ul>
            <p v-else class="mt-2 text-xs text-muted-foreground">{{ $t("projects.noLabels") }}</p>
            <form
              v-if="canLabel"
              class="mt-3 flex flex-wrap items-end gap-2"
              @submit.prevent="addLabel"
            >
              <Field :label="$ts('projects.newLabel')" class="min-w-40 flex-1">
                <template #default="{ id }">
                  <Input :id="id" v-model="newLabel" />
                </template>
              </Field>
              <fieldset class="flex gap-1 pb-1.5">
                <legend class="sr-only">{{ $t("projects.labelColour") }}</legend>
                <label v-for="colour in palette" :key="colour.value" class="cursor-pointer">
                  <input
                    v-model="newLabelColour"
                    type="radio"
                    name="label-colour"
                    class="peer sr-only"
                    :value="colour.value"
                  />
                  <span
                    class="block size-5 rounded-full ring-offset-2 ring-offset-background peer-checked:ring-2 peer-checked:ring-ring peer-focus-visible:ring-2 peer-focus-visible:ring-ring"
                    :style="{ backgroundColor: colour.value }"
                  />
                  <span class="sr-only">{{ $t(`projects.colour.${colour.name}`) }}</span>
                </label>
              </fieldset>
              <Button type="submit" :disabled="!newLabel.trim()">
                {{ $t("projects.addLabel") }}
              </Button>
            </form>
          </section>

          <section :aria-labelledby="'priorities-heading'">
            <h2 id="priorities-heading" class="text-sm font-semibold">
              {{ $t("projects.priority.scale") }}
            </h2>
            <p class="mt-0.5 text-xs text-muted-foreground">
              {{ $t("projects.priority.scaleHint") }}
            </p>
            <ol class="mt-3 space-y-1">
              <li
                v-for="(priority, index) in priorities"
                :key="priority.id"
                class="flex items-center gap-2 rounded-lg border bg-card px-3 py-1.5"
              >
                <ProjectsPriorityBadge :priority="priority" class="flex-1" />
                <template v-if="canConfigure">
                  <Button
                    variant="ghost"
                    size="icon"
                    :label="$ts('projects.priority.moreUrgent', { priority: priority.name })"
                    :disabled="index === 0"
                    @click="shiftPriority(index, -1)"
                  >
                    <Icon name="lucide:arrow-up" class="size-3.5" />
                  </Button>
                  <Button
                    variant="ghost"
                    size="icon"
                    :label="$ts('projects.priority.lessUrgent', { priority: priority.name })"
                    :disabled="index === priorities.length - 1"
                    @click="shiftPriority(index, 1)"
                  >
                    <Icon name="lucide:arrow-down" class="size-3.5" />
                  </Button>
                  <Button
                    variant="ghost"
                    size="icon"
                    :label="$ts('projects.priority.remove', { priority: priority.name })"
                    @click="attempt(() => projectsApi.removePriority(key, priority.id))"
                  >
                    <Icon name="lucide:trash-2" class="size-3.5" />
                  </Button>
                </template>
              </li>
            </ol>
            <form
              v-if="canConfigure"
              class="mt-3 flex flex-wrap items-end gap-2"
              @submit.prevent="addPriority"
            >
              <Field :label="$ts('projects.priority.new')" class="min-w-40 flex-1">
                <template #default="{ id }">
                  <Input :id="id" v-model="newPriority" />
                </template>
              </Field>
              <Button type="submit" :disabled="!newPriority.trim()">
                {{ $t("projects.priority.add") }}
              </Button>
            </form>
          </section>

          <section :aria-labelledby="'members-heading'">
            <h2 id="members-heading" class="text-sm font-semibold">{{ $t("projects.members") }}</h2>
            <ul class="mt-3 space-y-1">
              <li
                v-for="member in members"
                :key="member.username"
                class="flex items-center gap-2 rounded-lg px-2 py-1.5 hover:bg-accent"
              >
                <UiAvatar :name="nameOf(member)" size="sm" />
                <span class="min-w-0 flex-1">
                  <span class="block truncate text-sm">{{ nameOf(member) }}</span>
                  <span class="block truncate text-xs text-muted-foreground">{{
                    member.address
                  }}</span>
                </span>
                <span
                  v-if="member.remote"
                  class="inline-flex items-center gap-1 rounded-md bg-muted px-1.5 py-0.5 text-xs text-muted-foreground"
                  :title="$ts('projects.federation.remoteHint')"
                >
                  <Icon name="lucide:globe" class="size-3" aria-hidden="true" />
                  {{ $t("projects.federation.remote") }}
                </span>
                <span class="text-xs text-muted-foreground">
                  {{
                    member.role === "admin" ? $t("projects.roleAdmin") : $t("projects.roleMember")
                  }}
                </span>
                <Button
                  v-if="canAdminister"
                  variant="ghost"
                  size="icon"
                  :label="$ts('projects.removeMember', { who: nameOf(member) })"
                  @click="attempt(() => projectsApi.removeMember(key, member.username))"
                >
                  <Icon name="lucide:user-minus" class="size-3.5" />
                </Button>
              </li>
            </ul>
            <form
              v-if="canAdminister"
              class="mt-3 flex flex-wrap items-center gap-2"
              @submit.prevent="memberForm.handleSubmit()"
            >
              <memberForm.Field name="username">
                <template #default="{ field }">
                  <Field
                    :label="$ts('projects.username')"
                    :hint="project.federated ? $ts('projects.federation.addressHint') : undefined"
                    class="min-w-40 flex-1"
                    :error="
                      field.state.meta.isTouched && field.state.meta.errors.length
                        ? $ts('projects.usernameRequired')
                        : undefined
                    "
                  >
                    <template #default="{ id, invalid, describedBy }">
                      <Input
                        :id="id"
                        :model-value="field.state.value"
                        :invalid="invalid"
                        :described-by="describedBy"
                        autocomplete="off"
                        @update:model-value="field.handleChange($event)"
                        @blur="field.handleBlur"
                      />
                    </template>
                  </Field>
                </template>
              </memberForm.Field>
              <memberForm.Field name="role">
                <template #default="{ field }">
                  <Field :label="$ts('projects.role')" class="w-36">
                    <template #default="{ id }">
                      <Select
                        :id="id"
                        :model-value="field.state.value"
                        :label="$ts('projects.role')"
                        :options="roles"
                        @update:model-value="$event && field.handleChange($event)"
                      />
                    </template>
                  </Field>
                </template>
              </memberForm.Field>
              <Button type="submit" class="self-end">{{ $t("projects.addMember") }}</Button>
            </form>
          </section>

          <section v-if="sharable" :aria-labelledby="'federation-heading'">
            <h2 id="federation-heading" class="text-sm font-semibold">
              {{ $t("projects.federation.title") }}
            </h2>
            <div class="mt-2 flex items-start gap-2.5">
              <Switch
                v-model="federated"
                :disabled="!canAdminister"
                :label="$ts('projects.federation.label')"
                class="mt-0.5"
              />
              <div>
                <p class="text-xs">{{ $t("projects.federation.label") }}</p>
                <p class="text-xs text-muted-foreground">{{ $t("projects.federation.hint") }}</p>
              </div>
            </div>
          </section>

          <section v-if="canAdminister" :aria-labelledby="'visibility-heading'">
            <h2 id="visibility-heading" class="text-sm font-semibold">
              {{ $t("projects.visibility") }}
            </h2>
            <p class="mt-0.5 text-xs text-muted-foreground">{{ $t("projects.visibilityHint") }}</p>
            <Field :label="$ts('projects.visibility')" class="mt-2 w-48">
              <template #default="{ id }">
                <Select
                  :id="id"
                  :model-value="project.visibility"
                  :label="$ts('projects.visibility')"
                  :options="visibilities"
                  @update:model-value="setVisibility"
                />
              </template>
            </Field>
          </section>

          <section v-if="canAdminister" :aria-labelledby="'planning-heading'">
            <h2 id="planning-heading" class="text-sm font-semibold">
              {{ $t("projects.planning.settings") }}
            </h2>
            <p class="mt-0.5 text-xs text-muted-foreground">
              {{ $t("projects.planning.settingsHint") }}
            </p>
            <form class="mt-2 flex flex-wrap items-end gap-2" @submit.prevent="saveZone">
              <Field :label="$ts('projects.planning.timeZone')" class="w-64">
                <template #default="{ id }">
                  <Input :id="id" v-model="zone" placeholder="Africa/Dar_es_Salaam" />
                </template>
              </Field>
              <Button type="submit" variant="outline">{{ $t("action.save") }}</Button>
            </form>
            <Field :label="$ts('projects.planning.policy')" class="mt-3 w-64">
              <template #default="{ id }">
                <Select
                  :id="id"
                  :model-value="project.cyclePolicy"
                  :label="$ts('projects.planning.policy')"
                  :options="policies"
                  @update:model-value="setPolicy"
                />
              </template>
            </Field>
            <p class="mt-1 text-xs text-muted-foreground">
              {{ $t(`projects.planning.policyHints.${project.cyclePolicy}`) }}
            </p>
          </section>
        </div>
      </div>
    </template>

    <!-- A modal: a dialog is not a button, and Escape is its keyboard path. -->
    <!-- eslint-disable-next-line vuejs-accessibility/no-static-element-interactions -->
    <div
      v-if="removing"
      class="fixed inset-0 z-50 grid place-items-center bg-background/70 p-4 backdrop-blur-sm"
      role="dialog"
      aria-modal="true"
      :aria-label="$ts('projects.removeState', { state: removing.name })"
      @click.self="removing = null"
      @keydown.esc="removing = null"
    >
      <div class="w-[min(26rem,100%)] space-y-3 rounded-xl border bg-card p-4 shadow-2xl">
        <h2 class="text-sm font-semibold">
          {{ $t("projects.removeState", { state: removing.name }) }}
        </h2>
        <p class="text-xs text-muted-foreground">{{ $t("projects.removeStateHint") }}</p>
        <Field :label="$ts('projects.moveIssuesTo')">
          <template #default="{ id }">
            <Select
              :id="id"
              v-model="moveTo"
              :label="$ts('projects.moveIssuesTo')"
              :options="destinations"
            />
          </template>
        </Field>
        <div class="flex justify-end gap-2">
          <Button variant="ghost" @click="removing = null">{{ $t("action.cancel") }}</Button>
          <Button variant="danger" @click="remove">{{ $t("action.remove") }}</Button>
        </div>
      </div>
    </div>
  </div>
</template>
