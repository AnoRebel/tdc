<script setup lang="ts">
import { useForm, type AnyFieldApi } from "@tanstack/vue-form";
import * as v from "valibot";

import type { GrantablePermission } from "@tdc/types";
import { isApiError } from "@tdc/types";
import { Button } from "#layers/ui/app/components/ui/button";
import { Checkbox } from "#layers/ui/app/components/ui/checkbox";
import { Field } from "#layers/ui/app/components/ui/field";
import { Input } from "#layers/ui/app/components/ui/input";
import { Spinner } from "#layers/ui/app/components/ui/spinner";
import { Textarea } from "#layers/ui/app/components/ui/textarea";

/**
 * Registering an application: what it is, what it is for, what it wants, and
 * whether anything leaves the server.
 *
 * Registering grants nothing. It records a request somebody then approves,
 * having been shown all of this — which is why the disclosure is required the
 * moment the box saying data leaves is ticked, rather than left optional and
 * missing when it matters.
 */

const props = defineProps<{ grantable: GrantablePermission[] }>();
const emit = defineEmits<{ registered: [] }>();

const { $t, $ts } = useI18n();
const integrations = useIntegrations();
const error = ref<string | null>(null);

const schema = v.pipe(
  v.object({
    name: v.pipe(
      v.string(),
      v.trim(),
      v.minLength(1, $ts("integrations.appNameRequired")),
      v.maxLength(60, $ts("integrations.appNameTooLong")),
    ),
    purpose: v.pipe(
      v.string(),
      v.trim(),
      v.minLength(1, $ts("integrations.appPurposeRequired")),
      v.maxLength(500, $ts("integrations.appPurposeTooLong")),
    ),
    permissions: v.array(v.string()),
    sendsDataExternally: v.boolean(),
    externalDisclosure: v.string(),
  }),
  v.forward(
    v.check(
      (value) => !value.sendsDataExternally || value.externalDisclosure.trim().length > 0,
      $ts("integrations.disclosureRequired"),
    ),
    ["externalDisclosure"],
  ),
);

const form = useForm({
  defaultValues: {
    name: "",
    purpose: "",
    permissions: [] as string[],
    sendsDataExternally: false,
    externalDisclosure: "",
  },
  validators: { onSubmit: schema },
  async onSubmit({ value }) {
    error.value = null;
    try {
      await integrations.register(value);
      form.reset();
      emit("registered");
    } catch (cause) {
      error.value = isApiError(cause) ? cause.message : $ts("integrations.couldNotSave");
    }
  },
});

function firstError(field: AnyFieldApi): string | undefined {
  if (!field.state.meta.isTouched) return undefined;
  const [issue] = field.state.meta.errors;
  if (!issue) return undefined;
  return typeof issue === "string" ? issue : (issue as { message?: string }).message;
}

function toggle(field: AnyFieldApi, name: string, on: boolean) {
  const chosen = new Set(field.state.value as string[]);
  if (on) chosen.add(name);
  else chosen.delete(name);
  field.handleChange([...chosen].sort());
}
</script>

<template>
  <form
    class="space-y-3 rounded-md border p-3"
    :aria-label="$ts('integrations.registerApplication')"
    @submit.prevent="form.handleSubmit()"
  >
    <h3 class="text-sm font-medium">{{ $t("integrations.registerApplication") }}</h3>
    <p class="text-xs text-muted-foreground">{{ $t("integrations.registerHint") }}</p>

    <p
      v-if="error"
      class="rounded-md bg-destructive/10 px-3 py-2 text-sm text-destructive"
      role="alert"
    >
      {{ error }}
    </p>

    <form.Field name="name">
      <template #default="{ field }">
        <Field :label="$ts('integrations.appName')" :error="firstError(field)" required>
          <template #default="{ id, invalid, describedBy }">
            <Input
              :id="id"
              :model-value="field.state.value"
              :invalid="invalid"
              :described-by="describedBy"
              :placeholder="$ts('integrations.appNamePlaceholder')"
              @update:model-value="field.handleChange($event)"
              @blur="field.handleBlur"
            />
          </template>
        </Field>
      </template>
    </form.Field>

    <form.Field name="purpose">
      <template #default="{ field }">
        <Field :label="$ts('integrations.appPurpose')" :error="firstError(field)" required>
          <template #default="{ id, invalid, describedBy }">
            <Textarea
              :id="id"
              class="h-auto py-2"
              rows="2"
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

    <form.Field name="permissions">
      <template #default="{ field }">
        <fieldset>
          <legend class="mb-1.5 text-sm font-medium">
            {{ $t("integrations.appPermissions") }}
          </legend>
          <div class="max-h-56 space-y-1 overflow-y-auto rounded-md border p-2">
            <!-- eslint-disable-next-line vuejs-accessibility/label-has-for -- the checkbox is a component, which the rule cannot see inside -->
            <label
              v-for="permission in props.grantable"
              :key="permission.name"
              class="flex items-start gap-2 text-xs"
            >
              <Checkbox
                class="mt-0.5"
                :model-value="(field.state.value as string[]).includes(permission.name)"
                @update:model-value="(on: boolean) => toggle(field, permission.name, on)"
              />
              <span>
                <span class="font-mono font-medium">{{ permission.name }}</span>
                <span v-if="permission.administrative" class="ml-1 text-destructive">
                  ({{ $t("integrations.administrative") }})
                </span>
                <span class="block text-muted-foreground">{{ permission.description }}</span>
              </span>
            </label>
          </div>
        </fieldset>
      </template>
    </form.Field>

    <form.Field name="sendsDataExternally">
      <template #default="{ field }">
        <!-- eslint-disable-next-line vuejs-accessibility/label-has-for -- the checkbox is a component, which the rule cannot see inside -->
        <label class="flex items-center gap-2 text-sm">
          <Checkbox
            :model-value="field.state.value"
            @update:model-value="(on: boolean) => field.handleChange(on)"
          />
          {{ $t("integrations.sendsDataExternally") }}
        </label>
      </template>
    </form.Field>

    <form.Subscribe>
      <template #default="{ values }">
        <form.Field v-if="values.sendsDataExternally" name="externalDisclosure">
          <template #default="{ field }">
            <Field
              :label="$ts('integrations.externalDisclosure')"
              :hint="$ts('integrations.externalDisclosureHint')"
              :error="firstError(field)"
              required
            >
              <template #default="{ id, invalid, describedBy }">
                <Textarea
                  :id="id"
                  class="h-auto py-2"
                  rows="2"
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
      </template>
    </form.Subscribe>

    <div class="flex justify-end">
      <form.Subscribe>
        <template #default="{ isSubmitting }">
          <Button type="submit" :disabled="isSubmitting">
            <Spinner v-if="isSubmitting" size="sm" />
            {{ $t("integrations.register") }}
          </Button>
        </template>
      </form.Subscribe>
    </div>
  </form>
</template>
