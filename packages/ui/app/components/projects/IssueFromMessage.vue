<script setup lang="ts">
import { useForm } from "@tanstack/vue-form";
import type { AnyFieldApi } from "@tanstack/vue-form";
import * as v from "valibot";

import { isApiError, type Issue, type Project } from "@tdc/types";
import { Button } from "#layers/ui/app/components/ui/button";
import { Checkbox } from "#layers/ui/app/components/ui/checkbox";
import { Field } from "#layers/ui/app/components/ui/field";
import { Input } from "#layers/ui/app/components/ui/input";
import { Select } from "#layers/ui/app/components/ui/select";
import { Textarea } from "#layers/ui/app/components/ui/textarea";

const { $t, $ts, $localeRoute: localeRoute } = useI18n();

/**
 * Turning a message into an issue, without leaving the conversation.
 *
 * The title and description start as the message — its first line and the
 * rest — and are the author's to change. The message itself is kept as a
 * reference, not copied: the issue points at it, and anybody opening the issue
 * sees it only if they could read it where it was said.
 */
const props = defineProps<{ roomId: string; message: { id: string; body: string } }>();
const emit = defineEmits<{ close: []; filed: [issue: Issue] }>();

const projectsApi = useProjects();
const router = useRouter();

const projects = ref<Project[]>([]);
const loading = ref(true);
const refused = ref<string | null>(null);

onMounted(async () => {
  try {
    projects.value = (await projectsApi.list()).filter((project) => project.can?.createIssues);
    if (projects.value.length) form.setFieldValue("project", projects.value[0]!.key);
  } catch {
    refused.value = $ts("projects.couldNotLoad");
  } finally {
    loading.value = false;
  }
});

const [firstLine = "", ...rest] = props.message.body.trim().split("\n");
const title = firstLine.length > 200 ? `${firstLine.slice(0, 197)}…` : firstLine;
const description = (firstLine.length > 200 ? props.message.body.trim() : rest.join("\n")).trim();

const schema = v.object({
  project: v.pipe(v.string(), v.minLength(1, $ts("projects.fromMessage.chooseProject"))),
  title: v.pipe(
    v.string(),
    v.trim(),
    v.minLength(1, $ts("projects.titleRequired")),
    v.maxLength(200, $ts("projects.titleTooLong")),
  ),
  description: v.pipe(v.string(), v.maxLength(20000)),
  link: v.boolean(),
});

const form = useForm({
  defaultValues: { project: "", title, description, link: true },
  validators: { onSubmit: schema },
  async onSubmit({ value }) {
    refused.value = null;
    try {
      const issue = await projectsApi.file(value.project, {
        title: value.title.trim(),
        description: value.description.trim(),
        ...(value.link ? { originChannel: props.roomId, originEvent: props.message.id } : {}),
      });
      emit("filed", issue);
      await router.push(localeRoute({ name: "issues-ref", params: { ref: issue.key } }));
    } catch (cause) {
      refused.value = isApiError(cause) ? cause.message : $ts("projects.couldNotFile");
    }
  },
});

const options = computed(() =>
  projects.value.map((project) => ({
    value: project.key,
    label: `${project.key} · ${project.name}`,
  })),
);

function firstError(field: AnyFieldApi): string | undefined {
  if (!field.state.meta.isTouched) return undefined;
  const [issue] = field.state.meta.errors;
  if (!issue) return undefined;
  return typeof issue === "string" ? issue : (issue as { message?: string }).message;
}

onKeyStroke("Escape", () => emit("close"));
</script>

<template>
  <Teleport to="body">
    <!-- A modal: a dialog is not a button, and Escape is its keyboard path. -->
    <!-- eslint-disable-next-line vuejs-accessibility/no-static-element-interactions -->
    <div
      class="fixed inset-0 z-50 grid place-items-center bg-black/50 p-4 backdrop-blur-sm"
      role="dialog"
      aria-modal="true"
      :aria-label="$ts('projects.fromMessage.title')"
      @click.self="emit('close')"
    >
      <div class="w-[min(32rem,100%)] rounded-xl border bg-card p-4 shadow-2xl">
        <h2 class="text-sm font-semibold">{{ $t("projects.fromMessage.title") }}</h2>
        <p class="mt-0.5 text-xs text-muted-foreground">{{ $t("projects.fromMessage.hint") }}</p>

        <p v-if="!loading && !projects.length" class="mt-4 text-sm text-muted-foreground">
          {{ $t("projects.fromMessage.noProjects") }}
        </p>

        <form v-else class="mt-4 space-y-3" @submit.prevent="form.handleSubmit()">
          <form.Field name="project">
            <template #default="{ field }">
              <Field :label="$ts('projects.fromMessage.project')" :error="firstError(field)">
                <template #default="{ id }">
                  <Select
                    :id="id"
                    :model-value="field.state.value"
                    :label="$ts('projects.fromMessage.project')"
                    :options="options"
                    :disabled="loading"
                    @update:model-value="$event && field.handleChange($event)"
                  />
                </template>
              </Field>
            </template>
          </form.Field>

          <form.Field name="title">
            <template #default="{ field }">
              <Field :label="$ts('projects.issueTitle')" :error="firstError(field)" required>
                <template #default="{ id, invalid, describedBy }">
                  <Input
                    :id="id"
                    :model-value="field.state.value"
                    :invalid="invalid"
                    :described-by="describedBy"
                    @update:model-value="field.handleChange($event)"
                    @blur="field.handleBlur"
                  />
                </template>
              </Field>
            </template>
          </form.Field>

          <form.Field name="description">
            <template #default="{ field }">
              <Field :label="$ts('projects.description')" :error="firstError(field)">
                <template #default="{ id, invalid, describedBy }">
                  <Textarea
                    :id="id"
                    class="h-auto py-2"
                    rows="4"
                    :model-value="field.state.value"
                    :invalid="invalid"
                    :described-by="describedBy"
                    @update:model-value="field.handleChange($event)"
                    @blur="field.handleBlur"
                  />
                </template>
              </Field>
            </template>
          </form.Field>

          <form.Field name="link">
            <template #default="{ field }">
              <span class="flex items-start gap-2 text-xs">
                <Checkbox
                  id="issue-from-message-link"
                  class="mt-0.5"
                  :model-value="field.state.value"
                  @update:model-value="field.handleChange($event)"
                />
                <label for="issue-from-message-link">
                  <span class="block font-medium">{{ $t("projects.fromMessage.link") }}</span>
                  <span class="block text-muted-foreground">
                    {{ $t("projects.fromMessage.linkHint") }}
                  </span>
                </label>
              </span>
            </template>
          </form.Field>

          <p v-if="refused" class="text-sm text-destructive" role="alert">{{ refused }}</p>

          <div class="flex justify-end gap-2">
            <Button variant="ghost" @click="emit('close')">{{ $t("action.cancel") }}</Button>
            <Button type="submit" :disabled="loading">{{ $t("projects.fileIssue") }}</Button>
          </div>
        </form>
      </div>
    </div>
  </Teleport>
</template>
