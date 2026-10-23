<script setup lang="ts">
import { useForm } from "@tanstack/vue-form";
import type { AnyFieldApi } from "@tanstack/vue-form";
import * as v from "valibot";

import { isApiError, type TranslationProviderState } from "@tdc/types";
import { Button } from "#layers/ui/app/components/ui/button";
import { Field } from "#layers/ui/app/components/ui/field";
import { Input } from "#layers/ui/app/components/ui/input";
import { Spinner } from "#layers/ui/app/components/ui/spinner";

/**
 * Connecting one provider: its key, and the address or region it needs.
 *
 * The key field is always empty. A saved key never comes back from the server
 * — not whole, not its last four characters — so there is nothing to show,
 * and an empty key on an existing connection keeps the one saved: changing a
 * region should not mean finding the key again.
 */

const props = defineProps<{ provider: TranslationProviderState }>();
const emit = defineEmits<{ done: []; cancel: [] }>();

const { $t, $ts } = useI18n();
const translation = useTranslation();

const name = props.provider.name;
const needsKey = name !== "libretranslate" && !props.provider.hasKey;

const error = ref<string | null>(null);
const saving = ref(false);

const schema = v.object({
  apiKey: needsKey
    ? v.pipe(v.string(), v.trim(), v.minLength(1, $ts("translation.keyRequired")))
    : v.pipe(v.string(), v.trim()),
  endpoint:
    name === "libretranslate"
      ? v.pipe(
          v.string(),
          v.trim(),
          v.minLength(1, $ts("translation.endpointRequired")),
          // Over https only: the key travels with every request.
          v.regex(/^https:\/\/[^\s/]+/i, $ts("translation.endpointHttps")),
        )
      : v.string(),
  region: v.pipe(v.string(), v.trim()),
});

function firstError(field: AnyFieldApi): string | undefined {
  if (!field.state.meta.isTouched) return undefined;
  const [issue] = field.state.meta.errors;
  if (!issue) return undefined;
  return typeof issue === "string" ? issue : (issue as { message?: string }).message;
}

const form = useForm({
  defaultValues: {
    apiKey: "",
    endpoint: props.provider.endpoint ?? "",
    region: props.provider.region ?? "",
  },
  validators: { onSubmit: schema },
  async onSubmit({ value }) {
    error.value = null;
    saving.value = true;
    try {
      await translation.connect(name, {
        apiKey: value.apiKey.trim(),
        endpoint: value.endpoint.trim(),
        region: value.region.trim(),
      });
      emit("done");
    } catch (cause) {
      error.value = isApiError(cause) ? cause.message : $ts("translation.couldNotConnect");
    } finally {
      saving.value = false;
    }
  },
});
</script>

<template>
  <form class="mt-3 space-y-3" @submit.prevent="form.handleSubmit()">
    <p
      v-if="error"
      class="rounded-md bg-destructive/10 px-3 py-2 text-xs text-destructive"
      role="alert"
    >
      {{ error }}
    </p>

    <form.Field v-if="name === 'libretranslate'" name="endpoint">
      <template #default="{ field }">
        <Field
          :label="$ts('translation.endpoint')"
          :hint="$ts('translation.endpointHint')"
          :error="firstError(field)"
          required
        >
          <template #default="{ id, invalid, describedBy }">
            <Input
              :id="id"
              type="url"
              inputmode="url"
              placeholder="https://"
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

    <form.Field name="apiKey">
      <template #default="{ field }">
        <Field
          :label="$ts('translation.apiKey')"
          :hint="
            provider.hasKey
              ? $ts('translation.keyKeepHint')
              : name === 'libretranslate'
                ? $ts('translation.keyOptionalHint')
                : $ts('translation.keyHint')
          "
          :error="firstError(field)"
          :required="needsKey"
        >
          <template #default="{ id, invalid, describedBy }">
            <!-- A password field: a key on screen is a key in a screenshot. -->
            <Input
              :id="id"
              type="password"
              autocomplete="off"
              spellcheck="false"
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

    <form.Field v-if="name === 'microsoft'" name="region">
      <template #default="{ field }">
        <Field :label="$ts('translation.region')" :hint="$ts('translation.regionHint')">
          <template #default="{ id, describedBy }">
            <Input
              :id="id"
              placeholder="westeurope"
              :model-value="field.state.value"
              :described-by="describedBy"
              @update:model-value="field.handleChange($event)"
              @blur="field.handleBlur"
            />
          </template>
        </Field>
      </template>
    </form.Field>

    <div class="flex items-center gap-2">
      <Button type="submit" :disabled="saving">
        <Spinner v-if="saving" size="sm" />
        {{ $t("translation.save") }}
      </Button>
      <Button variant="ghost" :disabled="saving" @click="emit('cancel')">
        {{ $t("translation.cancel") }}
      </Button>
    </div>
  </form>
</template>
