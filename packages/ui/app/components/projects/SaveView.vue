<script setup lang="ts">
import { useForm } from "@tanstack/vue-form";
import type { AnyFieldApi } from "@tanstack/vue-form";
import * as v from "valibot";

import { isApiError, type SavedView, type ViewFilter } from "@tdc/types";
import { Button } from "#layers/ui/app/components/ui/button";
import { Checkbox } from "#layers/ui/app/components/ui/checkbox";
import { Field } from "#layers/ui/app/components/ui/field";
import { Input } from "#layers/ui/app/components/ui/input";

const { $t, $ts } = useI18n();

/**
 * Naming what is on the screen, so it can be come back to.
 *
 * What is kept is the question — the filters as names — never the issues it
 * found. Shared, it is the same question for everybody who opens it, and each
 * of them gets their own answer.
 */
const props = defineProps<{
  /** The project it views, or null for a view across projects. */
  projectKey: string | null;
  layout: "list" | "board";
  filter: ViewFilter;
  /** Whether sharing is offered: a project's views are shared by its members. */
  mayShare: boolean;
}>();

const emit = defineEmits<{ saved: [view: SavedView]; cancel: [] }>();

const projectsApi = useProjects();
const refused = ref<string | null>(null);
const uid = useId();

const schema = v.object({
  name: v.pipe(
    v.string(),
    v.trim(),
    v.minLength(1, $ts("projects.saved.nameRequired")),
    v.maxLength(60, $ts("projects.saved.nameTooLong")),
  ),
  shared: v.boolean(),
});

const form = useForm({
  defaultValues: { name: "", shared: false },
  validators: { onSubmit: schema },
  async onSubmit({ value }) {
    refused.value = null;
    try {
      const view = await projectsApi.saveView(props.projectKey, {
        name: value.name.trim(),
        layout: props.layout,
        filter: props.filter,
        shared: props.mayShare && value.shared,
      });
      emit("saved", view);
    } catch (cause) {
      refused.value = isApiError(cause) ? cause.message : $ts("projects.saved.couldNotSave");
    }
  },
});

function firstError(field: AnyFieldApi): string | undefined {
  if (!field.state.meta.isTouched) return undefined;
  const [issue] = field.state.meta.errors;
  if (!issue) return undefined;
  return typeof issue === "string" ? issue : (issue as { message?: string }).message;
}
</script>

<template>
  <section class="max-w-xl rounded-xl border bg-card p-4" :aria-label="$ts('projects.saved.save')">
    <form class="space-y-3" @submit.prevent="form.handleSubmit()">
      <form.Field name="name">
        <template #default="{ field }">
          <Field :label="$ts('projects.saved.name')" :error="firstError(field)" required>
            <template #default="{ id, invalid, describedBy }">
              <Input
                :id="id"
                :model-value="field.state.value"
                :invalid="invalid"
                :described-by="describedBy"
                :placeholder="$ts('projects.saved.namePlaceholder')"
                @update:model-value="field.handleChange($event)"
                @blur="field.handleBlur"
              />
            </template>
          </Field>
        </template>
      </form.Field>
      <form.Field v-if="props.mayShare" name="shared">
        <template #default="{ field }">
          <span class="flex items-start gap-2 text-xs">
            <Checkbox
              :id="`${uid}-shared`"
              class="mt-0.5"
              :model-value="field.state.value"
              @update:model-value="field.handleChange($event)"
            />
            <label :for="`${uid}-shared`">
              <span class="block font-medium">{{ $t("projects.saved.share") }}</span>
              <span class="block text-muted-foreground">{{ $t("projects.saved.shareHint") }}</span>
            </label>
          </span>
        </template>
      </form.Field>
      <p v-if="refused" class="text-sm text-destructive" role="alert">{{ refused }}</p>
      <div class="flex justify-end gap-2">
        <Button variant="ghost" @click="emit('cancel')">{{ $t("action.cancel") }}</Button>
        <Button type="submit">{{ $t("projects.saved.save") }}</Button>
      </div>
    </form>
  </section>
</template>
