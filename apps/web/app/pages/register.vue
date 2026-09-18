<script setup lang="ts">
import { useForm } from "@tanstack/vue-form";
import type { AnyFieldApi } from "@tanstack/vue-form";
import * as v from "valibot";

import { isApiError } from "@tdc/types";
import { Button } from "#layers/ui/app/components/ui/button";
import { Field } from "#layers/ui/app/components/ui/field";
import { Input } from "#layers/ui/app/components/ui/input";
import { Spinner } from "#layers/ui/app/components/ui/spinner";

definePageMeta({ layout: "auth" });

useHead({ title: "Create an account" });

const { register } = useAuth();
const router = useRouter();
const { $t, $ts, $localeRoute: localeRoute } = useI18n();

const error = ref<string | null>(null);
const submitting = ref(false);

/*
 * What an account needs, stated once.
 *
 * The server decides all of this again and has the last word — a client that
 * checked nothing would still be safe. What this buys is being told before
 * sending: an address that is not an address, or a password too short to be
 * accepted, is worth saying at the field rather than after a round trip.
 *
 * Optional means optional: an empty display name or email passes, because the
 * form sends nothing for them rather than an empty string the server would
 * have to interpret.
 */
const account = v.object({
  username: v.pipe(
    v.string(),
    v.trim(),
    v.minLength(1, "Choose a username."),
    v.regex(/^[a-z0-9_.-]+$/i, "Letters, digits, underscore, dot and hyphen."),
  ),
  displayName: v.string(),
  email: v.union([
    v.literal(""),
    v.pipe(v.string(), v.trim(), v.email("That is not an email address.")),
  ]),
  password: v.pipe(v.string(), v.minLength(12, "At least twelve characters.")),
});

/** The first thing wrong with a field, once somebody has touched it. */
function firstError(field: AnyFieldApi): string | undefined {
  if (!field.state.meta.isTouched) return undefined;
  const [issue] = field.state.meta.errors;
  if (!issue) return undefined;
  return typeof issue === "string" ? issue : (issue as { message?: string }).message;
}

const form = useForm({
  defaultValues: { username: "", displayName: "", email: "", password: "" },
  validators: { onSubmit: account },
  async onSubmit({ value }) {
    error.value = null;
    submitting.value = true;

    try {
      await register({
        username: value.username,
        password: value.password,
        email: value.email || undefined,
        displayName: value.displayName || undefined,
      });
      await router.push(localeRoute({ name: "index" }));
    } catch (cause) {
      // The server names which field is the problem — a taken username, a weak
      // password — so its message is shown rather than a generic one.
      error.value = isApiError(cause) ? cause.message : "Could not reach the server.";
    } finally {
      submitting.value = false;
    }
  },
});
</script>

<template>
  <div>
    <h1 class="text-xl font-semibold">{{ $t("auth.createAnAccount") }}</h1>
    <p class="mt-1 text-sm text-muted-foreground">
      {{ $t("auth.usernameBecomesAddress") }}
    </p>

    <form class="mt-6 space-y-4" @submit.prevent="form.handleSubmit()">
      <p
        v-if="error"
        class="rounded-md bg-destructive/10 px-3 py-2 text-sm text-destructive"
        role="alert"
      >
        {{ error }}
      </p>

      <form.Field name="username">
        <template #default="{ field }">
          <Field :label="$ts('auth.username')" :error="firstError(field)" required>
            <template #default="{ id, invalid, describedBy }">
              <Input
                :id="id"
                :model-value="field.state.value"
                autofocus
                autocomplete="username"
                :invalid="invalid"
                :described-by="describedBy"
                @update:model-value="field.handleChange($event)"
                @blur="field.handleBlur"
              />
            </template>
          </Field>
        </template>
      </form.Field>

      <form.Field name="displayName">
        <template #default="{ field }">
          <Field
            :label="$ts('auth.displayName')"
            :hint="$ts('auth.optional')"
            :error="firstError(field)"
          >
            <template #default="{ id, invalid, describedBy }">
              <Input
                :id="id"
                :model-value="field.state.value"
                autocomplete="name"
                :invalid="invalid"
                :described-by="describedBy"
                @update:model-value="field.handleChange($event)"
                @blur="field.handleBlur"
              />
            </template>
          </Field>
        </template>
      </form.Field>

      <form.Field name="email">
        <template #default="{ field }">
          <Field :label="$ts('auth.email')" :hint="$ts('auth.optional')" :error="firstError(field)">
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

      <form.Field name="password">
        <template #default="{ field }">
          <Field
            :label="$ts('auth.password')"
            :hint="$ts('auth.atLeastTwelveCharacters')"
            :error="firstError(field)"
            required
          >
            <template #default="{ id, invalid, describedBy }">
              <Input
                :id="id"
                type="password"
                :model-value="field.state.value"
                autocomplete="new-password"
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
        {{ submitting ? $t("auth.creatingAccount") : $t("auth.createAccount") }}
      </Button>
    </form>

    <p class="mt-6 text-center text-sm text-muted-foreground">
      {{ $t("auth.haveAccount") }}
      <NuxtLink
        :to="localeRoute({ name: 'signin' })"
        class="font-medium text-primary hover:underline"
      >
        {{ $t("auth.signIn") }}
      </NuxtLink>
    </p>
  </div>
</template>
