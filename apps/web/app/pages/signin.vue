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

useHead({ title: "Sign in" });

const { signIn, setupStatus } = useAuth();
const router = useRouter();
const { $localeRoute: localeRoute } = useI18n();

const error = ref<string | null>(null);
const submitting = ref(false);

/*
 * What a sign-in needs before it is worth sending.
 *
 * The same schema the desktop client uses, for the same reason: the rule is
 * stated once and the message beside a field is the string the rule carries.
 * Whether the credentials are right is the server's to say; this only decides
 * whether there is anything to ask about.
 */
const credentials = v.object({
  username: v.pipe(v.string(), v.trim(), v.minLength(1, "Your username or email goes here.")),
  password: v.pipe(v.string(), v.minLength(1, "Your password goes here.")),
});

/** The first thing wrong with a field, once somebody has touched it. */
function firstError(field: AnyFieldApi): string | undefined {
  if (!field.state.meta.isTouched) return undefined;
  const [issue] = field.state.meta.errors;
  if (!issue) return undefined;
  return typeof issue === "string" ? issue : (issue as { message?: string }).message;
}

/*
 * A server with no accounts cannot be signed into, so send the first visitor
 * to setup rather than showing them a form that can only fail.
 *
 * Guarded because this awaits during setup: an unhandled rejection here is an
 * error page rather than a form, and this page in particular has to work with
 * no connection — it is where somebody lands when their session could not be
 * confirmed.
 */
try {
  const status = await setupStatus();
  if (status.setupRequired) {
    await router.replace(localeRoute({ name: "setup" }));
  }
} catch {
  // Unknown, so the form is shown. Signing in will report the same problem
  // more usefully, against something the person actually did.
}

const form = useForm({
  defaultValues: { username: "", password: "" },
  validators: { onSubmit: credentials },
  async onSubmit({ value }) {
    error.value = null;
    submitting.value = true;

    try {
      await signIn({ username: value.username, password: value.password });

      /*
       * Back to whatever they were trying to reach.
       *
       * A path from the query is followed as given — it is a route inside this
       * application, recorded by the middleware that sent them here — and
       * anything else goes home by name.
       */
      const next = router.currentRoute.value.query.next;
      await router.push(typeof next === "string" ? next : localeRoute({ name: "index" }));
    } catch (cause) {
      if (isApiError(cause) && cause.code === "rate-limited") {
        const wait = cause.retryAfterSeconds;
        error.value = wait
          ? `Too many attempts. Try again in ${Math.ceil(wait / 60)} minute(s).`
          : "Too many attempts. Try again later.";
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
    <h1 class="text-xl font-semibold">Sign in</h1>
    <p class="mt-1 text-sm text-muted-foreground">Welcome back.</p>

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
          <Field label="Username or email" :error="firstError(field)" required>
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

      <form.Field name="password">
        <template #default="{ field }">
          <Field label="Password" :error="firstError(field)" required>
            <template #default="{ id, invalid, describedBy }">
              <Input
                :id="id"
                type="password"
                :model-value="field.state.value"
                autocomplete="current-password"
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
        {{ submitting ? "Signing in" : "Sign in" }}
      </Button>
    </form>

    <p class="mt-6 text-center text-sm text-muted-foreground">
      No account yet?
      <NuxtLink
        :to="localeRoute({ name: 'register' })"
        class="font-medium text-primary hover:underline"
      >
        Create one
      </NuxtLink>
    </p>
  </div>
</template>
