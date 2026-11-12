<script setup lang="ts">
import { useForm } from "@tanstack/vue-form";
import type { AnyFieldApi } from "@tanstack/vue-form";
import * as v from "valibot";

import type { Company } from "@tdc/types";
import { isApiError } from "@tdc/types";
import { Button } from "#layers/ui/app/components/ui/button";
import { Field } from "#layers/ui/app/components/ui/field";
import { Input } from "#layers/ui/app/components/ui/input";
import { Textarea } from "#layers/ui/app/components/ui/textarea";
import { Spinner } from "#layers/ui/app/components/ui/spinner";
import VisibilityPicker from "#layers/ui/app/components/directory/VisibilityPicker.vue";

const { $t, $ts } = useI18n();

/**
 * Creating a company, or changing one somebody administers.
 *
 * The address is chosen once, at creation, and never changes afterwards: links
 * to the company would break, and a company that wants a different address is,
 * as far as anybody holding the old link can tell, a different company.
 */

const props = defineProps<{
  /** The company being changed; absent when creating one. */
  company?: Company;
}>();

const emit = defineEmits<{ saved: [Company] }>();

const directory = useDirectory();
const error = ref<string | null>(null);

const schema = computed(() =>
  v.object({
    slug: props.company
      ? v.string()
      : v.pipe(
          v.string(),
          v.trim(),
          v.regex(/^[a-z0-9](?:[a-z0-9-]{0,38}[a-z0-9])?$/, $ts("directory.company.slugRule")),
        ),
    name: v.pipe(
      v.string(),
      v.trim(),
      v.minLength(1, $ts("directory.company.nameRequired")),
      v.maxLength(80, $ts("directory.form.tooLong", { count: 80 })),
    ),
    description: v.pipe(
      v.string(),
      v.maxLength(2000, $ts("directory.form.tooLong", { count: 2000 })),
    ),
    location: v.pipe(v.string(), v.maxLength(80, $ts("directory.form.tooLong", { count: 80 }))),
    visibility: v.picklist(["public", "members"]),
    role: v.pipe(v.string(), v.maxLength(60, $ts("directory.form.tooLong", { count: 60 }))),
  }),
);

function firstError(field: AnyFieldApi): string | undefined {
  if (!field.state.meta.isTouched && !form.state.submissionAttempts) return undefined;
  const [issue] = field.state.meta.errors;
  if (!issue) return undefined;
  return typeof issue === "string" ? issue : (issue as { message?: string }).message;
}

const form = useForm({
  defaultValues: {
    slug: props.company?.slug ?? "",
    name: props.company?.name ?? "",
    description: props.company?.description ?? "",
    location: props.company?.location ?? "",
    visibility: (props.company?.visibility ?? "members") as "public" | "members",
    role: "",
  },
  validators: { onSubmit: schema.value },
  async onSubmit({ value }) {
    error.value = null;
    try {
      const saved = props.company
        ? await directory.updateCompany(props.company.slug, value)
        : await directory.createCompany(value);
      emit("saved", saved);
    } catch (cause) {
      error.value = isApiError(cause) ? cause.message : $ts("directory.couldNotReach");
    }
  },
});
</script>

<template>
  <form class="space-y-4" novalidate @submit.prevent="form.handleSubmit()">
    <p
      v-if="error"
      class="rounded-md bg-destructive/10 px-3 py-2 text-sm text-destructive"
      role="alert"
    >
      {{ error }}
    </p>

    <div class="grid gap-4 sm:grid-cols-2">
      <form.Field name="name">
        <template #default="{ field }">
          <Field :label="$ts('directory.company.name')" :error="firstError(field)" required>
            <template #default="{ id, invalid, describedBy }">
              <Input
                :id="id"
                :model-value="field.state.value"
                autocomplete="organization"
                :invalid="invalid"
                :described-by="describedBy"
                @update:model-value="field.handleChange($event)"
                @blur="field.handleBlur"
              />
            </template>
          </Field>
        </template>
      </form.Field>

      <form.Field v-if="!props.company" name="slug">
        <template #default="{ field }">
          <Field
            :label="$ts('directory.company.slug')"
            :hint="$ts('directory.company.slugHint')"
            :error="firstError(field)"
            required
          >
            <template #default="{ id, invalid, describedBy }">
              <Input
                :id="id"
                :model-value="field.state.value"
                autocomplete="off"
                :invalid="invalid"
                :described-by="describedBy"
                @update:model-value="field.handleChange($event.toLowerCase())"
                @blur="field.handleBlur"
              />
            </template>
          </Field>
        </template>
      </form.Field>

      <form.Field name="location">
        <template #default="{ field }">
          <Field :label="$ts('directory.profile.location')" :error="firstError(field)">
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

      <form.Field v-if="!props.company" name="role">
        <template #default="{ field }">
          <Field
            :label="$ts('directory.company.yourRole')"
            :hint="$ts('directory.company.roleHint')"
            :error="firstError(field)"
          >
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
    </div>

    <form.Field name="description">
      <template #default="{ field }">
        <Field :label="$ts('directory.company.description')" :error="firstError(field)">
          <template #default="{ id, invalid, describedBy }">
            <Textarea
              :id="id"
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

    <form.Field name="visibility">
      <template #default="{ field }">
        <VisibilityPicker
          :model-value="field.state.value"
          :legend="$ts('directory.company.whoCanSee')"
          :choices="['public', 'members']"
          @update:model-value="field.handleChange($event as 'public' | 'members')"
        />
      </template>
    </form.Field>

    <form.Subscribe>
      <template #default="{ isSubmitting }">
        <div class="flex justify-end">
          <Button type="submit" class="h-9" :disabled="isSubmitting">
            <Spinner v-if="isSubmitting" size="sm" />
            {{ props.company ? $t("directory.company.save") : $t("directory.company.create") }}
          </Button>
        </div>
      </template>
    </form.Subscribe>
  </form>
</template>
