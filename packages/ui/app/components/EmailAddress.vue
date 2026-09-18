<script setup lang="ts">
import { useForm } from "@tanstack/vue-form";
import type { AnyFieldApi } from "@tanstack/vue-form";
import * as v from "valibot";
import { AtSign, CircleCheck, CircleDashed } from "lucide-vue-next";

import { isApiError } from "@tdc/types";
import type { EmailState } from "@tdc/types";
import { Button } from "#layers/ui/app/components/ui/button";
import { Field } from "#layers/ui/app/components/ui/field";
import { Input } from "#layers/ui/app/components/ui/input";
import { Spinner } from "#layers/ui/app/components/ui/spinner";

const { $t, $ts } = useI18n();

/**
 * Somebody's own email address, and proving it belongs to them.
 *
 * Worth its own section rather than a field among preferences, because of what
 * it decides: an unconfirmed address means an account that cannot be recovered,
 * and somebody should be able to see which of those they have.
 */

const { emailState, confirmAddress } = useRecovery();

const state = ref<EmailState | null>(null);
const loading = ref(false);
const error = ref<string | null>(null);
const sent = ref(false);

async function load() {
  loading.value = true;
  try {
    state.value = await emailState();
  } catch (cause) {
    error.value = isApiError(cause) ? cause.message : "Could not read your address.";
  } finally {
    loading.value = false;
  }
}

const address = v.object({
  email: v.pipe(
    v.string(),
    v.trim(),
    v.minLength(1, "An address to send the link to."),
    // Shallow on purpose: only sending proves an address, and a stricter rule
    // refuses ones that work.
    v.regex(/.+@.+\..+/, "That does not look like an email address."),
  ),
});

function firstError(field: AnyFieldApi): string | undefined {
  if (!field.state.meta.isTouched) return undefined;
  const [issue] = field.state.meta.errors;
  if (!issue) return undefined;
  return typeof issue === "string" ? issue : (issue as { message?: string }).message;
}

const form = useForm({
  defaultValues: { email: "" },
  validators: { onSubmit: address },
  async onSubmit({ value }) {
    error.value = null;
    sent.value = false;

    try {
      await confirmAddress(value.email);
      sent.value = true;
      form.reset();
      await load();
    } catch (cause) {
      error.value = isApiError(cause) ? cause.message : "Could not send the link.";
    }
  },
});

onMounted(load);
</script>

<template>
  <section v-if="state?.canSendMail" class="space-y-4">
    <div>
      <h2 class="flex items-center gap-1.5 text-sm font-semibold">
        <AtSign class="size-4" />
        {{ $t("email.title") }}
      </h2>
      <p class="mt-1 text-xs text-muted-foreground">{{ $t("email.why") }}</p>
    </div>

    <p
      v-if="error"
      class="rounded-md bg-destructive/10 px-3 py-2 text-sm text-destructive"
      role="alert"
    >
      {{ error }}
    </p>

    <p v-if="loading" class="flex items-center gap-2 text-sm text-muted-foreground">
      <Spinner size="sm" />
      {{ $t("email.reading") }}
    </p>

    <template v-else>
      <!-- What they have, and whether it does anything. -->
      <p v-if="state.address" class="flex items-center gap-2 text-sm">
        <CircleCheck
          v-if="state.confirmed"
          class="size-4 shrink-0 text-primary"
          aria-hidden="true"
        />
        <CircleDashed v-else class="size-4 shrink-0 text-muted-foreground" aria-hidden="true" />
        <span class="truncate">{{ state.address }}</span>
        <span class="shrink-0 text-xs text-muted-foreground">
          {{ state.confirmed ? $t("email.confirmed") : $t("email.notConfirmed") }}
        </span>
      </p>
      <p v-else class="text-sm text-muted-foreground">{{ $t("email.none") }}</p>

      <!-- Waiting on one, so somebody who asked yesterday is reminded rather
           than left wondering why nothing changed. -->
      <p v-if="state.pending" class="rounded-md bg-muted px-3 py-2 text-sm" role="status">
        {{ $t("email.pendingBefore") }}
        <span class="font-medium">{{ state.pending }}</span
        >{{ $t("email.pendingAfter") }}
      </p>

      <p v-if="sent" class="rounded-md bg-primary/10 px-3 py-2 text-sm" role="status">
        {{ $t("email.sent") }}
      </p>

      <form class="flex items-end gap-2" @submit.prevent="form.handleSubmit()">
        <div class="flex-1">
          <form.Field name="email">
            <template #default="{ field }">
              <Field
                :label="state.address ? $ts('email.changeTo') : $ts('email.addAddress')"
                :error="firstError(field)"
              >
                <template #default="{ id, invalid, describedBy }">
                  <Input
                    :id="id"
                    type="email"
                    :model-value="field.state.value"
                    autocomplete="email"
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

        <Button
          type="submit"
          class="h-9"
          :disabled="form.state.isSubmitting"
          :label="$ts('email.sendConfirmationLink')"
        >
          <Spinner v-if="form.state.isSubmitting" size="sm" />
          {{ $t("email.sendLink") }}
        </Button>
      </form>
    </template>
  </section>
</template>
