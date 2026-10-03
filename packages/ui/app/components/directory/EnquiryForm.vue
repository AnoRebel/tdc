<script setup lang="ts">
import { useForm } from "@tanstack/vue-form";
import type { AnyFieldApi } from "@tanstack/vue-form";
import * as v from "valibot";
import { Send } from "lucide-vue-next";

import type { Enquiry } from "@tdc/types";
import { isApiError } from "@tdc/types";
import { Button } from "#layers/ui/app/components/ui/button";
import { Field } from "#layers/ui/app/components/ui/field";
import { Input } from "#layers/ui/app/components/ui/input";
import { Textarea } from "#layers/ui/app/components/ui/textarea";
import { Spinner } from "#layers/ui/app/components/ui/spinner";

const { $t, $ts } = useI18n();

/**
 * An enquiry to somebody, sent from their profile.
 *
 * One message: what the work is, and whatever context the person needs to
 * decide whether to take it up. Nothing more can be added until they answer,
 * so the form says so — somebody expecting a chat would otherwise wonder why
 * they cannot follow up.
 */

const props = defineProps<{ username: string; name: string }>();
const emit = defineEmits<{ sent: [Enquiry] }>();

const directory = useDirectory();
const error = ref<string | null>(null);

function firstError(field: AnyFieldApi): string | undefined {
  if (!field.state.meta.isTouched && !form.state.submissionAttempts) return undefined;
  const [issue] = field.state.meta.errors;
  if (!issue) return undefined;
  return typeof issue === "string" ? issue : (issue as { message?: string }).message;
}

const form = useForm({
  defaultValues: { subject: "", body: "" },
  validators: {
    onSubmit: v.object({
      subject: v.pipe(
        v.string(),
        v.trim(),
        v.minLength(1, $ts("directory.enquiry.subjectRequired")),
        v.maxLength(120, $ts("directory.form.tooLong", { count: 120 })),
      ),
      body: v.pipe(
        v.string(),
        v.trim(),
        v.minLength(1, $ts("directory.enquiry.bodyRequired")),
        v.maxLength(4000, $ts("directory.form.tooLong", { count: 4000 })),
      ),
    }),
  },
  async onSubmit({ value }) {
    error.value = null;
    try {
      emit("sent", await directory.sendEnquiry(props.username, value.subject, value.body));
    } catch (cause) {
      error.value =
        isApiError(cause) && cause.code === "rate-limited"
          ? $ts("directory.enquiry.slowDown")
          : isApiError(cause)
            ? cause.message
            : $ts("directory.couldNotReach");
    }
  },
});
</script>

<template>
  <form class="space-y-3" novalidate @submit.prevent="form.handleSubmit()">
    <p class="text-xs text-muted-foreground">
      {{ $t("directory.enquiry.oneMessage", { name: props.name }) }}
    </p>
    <p
      v-if="error"
      class="rounded-md bg-destructive/10 px-3 py-2 text-sm text-destructive"
      role="alert"
    >
      {{ error }}
    </p>

    <form.Field name="subject">
      <template #default="{ field }">
        <Field :label="$ts('directory.enquiry.subject')" :error="firstError(field)" required>
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

    <form.Field name="body">
      <template #default="{ field }">
        <Field
          :label="$ts('directory.enquiry.body')"
          :hint="$ts('directory.enquiry.bodyHint')"
          :error="firstError(field)"
          required
        >
          <template #default="{ id, invalid, describedBy }">
            <Textarea
              :id="id"
              rows="6"
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

    <form.Subscribe>
      <template #default="{ isSubmitting }">
        <div class="flex justify-end">
          <Button type="submit" class="h-9" :disabled="isSubmitting">
            <Spinner v-if="isSubmitting" size="sm" />
            <Send v-else class="size-3.5" />
            {{ $t("directory.enquiry.send") }}
          </Button>
        </div>
      </template>
    </form.Subscribe>
  </form>
</template>
