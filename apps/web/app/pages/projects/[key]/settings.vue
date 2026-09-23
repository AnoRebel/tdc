<script setup lang="ts">
import { useForm } from "@tanstack/vue-form";
import * as v from "valibot";

import { isApiError, type IssueState, type Project, type ProjectMember } from "@tdc/types";
import { Button } from "#layers/ui/app/components/ui/button";
import { Checkbox } from "#layers/ui/app/components/ui/checkbox";
import { Field } from "#layers/ui/app/components/ui/field";
import { Input } from "#layers/ui/app/components/ui/input";
import { Select } from "#layers/ui/app/components/ui/select";

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

/* Who can see it. */
const visibilities = computed(() => [
  { value: "public" as const, label: $ts("projects.public") },
  { value: "private" as const, label: $ts("projects.private") },
]);
async function setVisibility(visibility: "public" | "private" | undefined) {
  if (!visibility || visibility === project.value?.visibility) return;
  await attempt(() => projectsApi.update(key.value, { visibility }));
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

          <section :aria-labelledby="'members-heading'">
            <h2 id="members-heading" class="text-sm font-semibold">{{ $t("projects.members") }}</h2>
            <ul class="mt-3 space-y-1">
              <li
                v-for="member in members"
                :key="member.username"
                class="flex items-center gap-2 rounded-lg px-2 py-1.5 hover:bg-accent"
              >
                <UiAvatar :name="member.displayName || member.username" size="sm" />
                <span class="min-w-0 flex-1">
                  <span class="block truncate text-sm">{{
                    member.displayName || member.username
                  }}</span>
                  <span class="block truncate text-xs text-muted-foreground">{{
                    member.address
                  }}</span>
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
                  :label="$ts('projects.removeMember', { who: member.username })"
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
