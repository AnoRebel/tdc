<script setup lang="ts">
import { useForm } from "@tanstack/vue-form";
import type { AnyFieldApi } from "@tanstack/vue-form";
import * as v from "valibot";

import type { ProposalVersion } from "@tdc/types";
import { Button } from "#layers/ui/app/components/ui/button";
import { Field } from "#layers/ui/app/components/ui/field";
import { Select } from "#layers/ui/app/components/ui/select";
import { Textarea } from "#layers/ui/app/components/ui/textarea";

const { $t, $ts } = useI18n();

/**
 * Asking for a change to one part of a proposal.
 *
 * One part per request — the work, the timeline, the currency or a single
 * line — so the person revising knows exactly what was asked about, and the
 * history shows it against that part rather than as a general complaint.
 */

const props = defineProps<{ version: ProposalVersion }>();
const emit = defineEmits<{ send: [part: string, comment: string]; cancel: [] }>();

const parts = computed(() => [
  { value: "scope", label: $ts("directory.agreement.parts.scope") },
  { value: "timeline", label: $ts("directory.agreement.parts.timeline") },
  { value: "currency", label: $ts("directory.agreement.parts.currency") },
  ...props.version.lineItems.map((item, index) => ({
    value: `item:${index + 1}`,
    label: `${$ts("directory.agreement.parts.item", { number: index + 1 })}: ${item.description}`,
  })),
]);

function firstError(field: AnyFieldApi): string | undefined {
  if (!field.state.meta.isTouched && !form.state.submissionAttempts) return undefined;
  const [issue] = field.state.meta.errors;
  if (!issue) return undefined;
  return typeof issue === "string" ? issue : (issue as { message?: string }).message;
}

const form = useForm({
  defaultValues: { part: "scope", comment: "" },
  validators: {
    onSubmit: v.object({
      part: v.pipe(v.string(), v.regex(/^(scope|timeline|currency|item:\d{1,2})$/)),
      comment: v.pipe(
        v.string(),
        v.trim(),
        v.minLength(1, $ts("directory.agreement.commentRequired")),
        v.maxLength(1000, $ts("directory.form.tooLong", { count: 1000 })),
      ),
    }),
  },
  onSubmit({ value }) {
    emit("send", value.part, value.comment.trim());
  },
});
</script>

<template>
  <form
    class="space-y-3 rounded-lg border p-3"
    novalidate
    data-change-request
    @submit.prevent="form.handleSubmit()"
  >
    <form.Field name="part">
      <template #default="{ field }">
        <Field :label="$ts('directory.agreement.part')" required>
          <template #default>
            <Select
              :model-value="field.state.value"
              :options="parts"
              :label="$ts('directory.agreement.part')"
              @update:model-value="field.handleChange(String($event))"
            />
          </template>
        </Field>
      </template>
    </form.Field>
    <form.Field name="comment">
      <template #default="{ field }">
        <Field :label="$ts('directory.agreement.comment')" :error="firstError(field)" required>
          <template #default="{ id, invalid, describedBy }">
            <Textarea
              :id="id"
              rows="3"
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
    <div class="flex justify-end gap-2">
      <Button variant="ghost" @click="emit('cancel')">{{ $t("action.cancel") }}</Button>
      <Button type="submit">{{ $t("directory.agreement.sendRequest") }}</Button>
    </div>
  </form>
</template>
