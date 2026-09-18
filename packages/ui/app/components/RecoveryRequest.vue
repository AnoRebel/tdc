<script setup lang="ts">
import { useForm } from "@tanstack/vue-form";
import type { AnyFieldApi } from "@tanstack/vue-form";
import * as v from "valibot";
import { MailCheck } from "lucide-vue-next";

import { isApiError } from "@tdc/types";
import { Button } from "#layers/ui/app/components/ui/button";
import { Field } from "#layers/ui/app/components/ui/field";
import { Input } from "#layers/ui/app/components/ui/input";
import { Spinner } from "#layers/ui/app/components/ui/spinner";

/**
 * Asking for a way back into an account.
 *
 * What is shown afterwards says nothing about whether an account was found,
 * because the server says nothing either — and a client that showed "no account
 * with that address" would reintroduce exactly the disclosure the server
 * refuses.
 */

const emit = defineEmits<{ cancel: [] }>();

const { request } = useRecovery();

const sent = ref(false);
const error = ref<string | null>(null);
const submitting = ref(false);

const details = v.object({
  email: v.pipe(
    v.string(),
    v.trim(),
    v.minLength(1, "The email address on your account."),
    // Shallow on purpose: only sending proves an address, and a strict rule
    // refuses ones that work.
    v.regex(/.+@.+/, "That does not look like an email address."),
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
  validators: { onSubmit: details },
  async onSubmit({ value }) {
    error.value = null;
    submitting.value = true;

    try {
      await request(value.email);
      sent.value = true;
    } catch (cause) {
      if (isApiError(cause) && cause.code === "rate-limited") {
        const wait = cause.retryAfterSeconds;
        error.value = wait
          ? `Too many requests. Try again in ${Math.ceil(wait / 60)} minute(s).`
          : "Too many requests. Try again later.";
      } else if (isApiError(cause)) {
        error.value = cause.message;
      } else {
        error.value = "Could not reach the server.";
      }
    } finally {
      submitting.value = false;
    }
  },
});
</script>

<template>
  <div>
    <!-- Sent, whatever was found. -->
    <template v-if="sent">
      <div class="flex gap-3">
        <span class="grid size-9 shrink-0 place-items-center rounded-lg bg-primary/12">
          <MailCheck class="size-4 text-primary" aria-hidden="true" />
        </span>
        <div class="min-w-0">
          <h2 class="text-sm font-semibold">Check your email</h2>
          <p class="mt-1 text-sm text-muted-foreground">
            If there is an account with that address, a link to set a new password is on its way. It
            works once, and for an hour.
          </p>
        </div>
      </div>

      <p class="mt-4 text-xs text-muted-foreground">
        Nothing arrived? Check the address you typed, and the spam folder. If the account has no
        confirmed email address, an administrator is the only way back in.
      </p>

      <Button
        variant="outline"
        class="mt-5 h-9 w-full"
        label="Back to sign in"
        @click="emit('cancel')"
      >
        Back to sign in
      </Button>
    </template>

    <template v-else>
      <h2 class="text-sm font-semibold">Forgotten your password</h2>
      <p class="mt-1 text-sm text-muted-foreground">
        Give the email address on your account and we will send a link to set a new password.
      </p>

      <p
        v-if="error"
        class="mt-4 rounded-md bg-destructive/10 px-3 py-2 text-sm text-destructive"
        role="alert"
      >
        {{ error }}
      </p>

      <form class="mt-5 space-y-4" @submit.prevent="form.handleSubmit()">
        <form.Field name="email">
          <template #default="{ field }">
            <Field label="Email address" :error="firstError(field)" required>
              <template #default="{ id, invalid, describedBy }">
                <Input
                  :id="id"
                  type="email"
                  :model-value="field.state.value"
                  autofocus
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

        <Button class="h-9 w-full" type="submit" :disabled="submitting">
          <Spinner v-if="submitting" size="sm" />
          {{ submitting ? "Sending" : "Send the link" }}
        </Button>
      </form>

      <Button
        variant="ghost"
        class="mt-2 h-9 w-full"
        label="Back to sign in"
        :disabled="submitting"
        @click="emit('cancel')"
      >
        Back to sign in
      </Button>
    </template>
  </div>
</template>
