<script setup lang="ts">
import { useForm } from "@tanstack/vue-form";
import type { AnyFieldApi } from "@tanstack/vue-form";
import * as v from "valibot";

import { isApiError } from "@tdc/types";
import { Button } from "#layers/ui/app/components/ui/button";
import { Checkbox } from "#layers/ui/app/components/ui/checkbox";
import { Field } from "#layers/ui/app/components/ui/field";
import { Input } from "#layers/ui/app/components/ui/input";
import * as Api from "#bindings/api";

/**
 * Signing in to the chosen server.
 *
 * The form and what it does are the web client's, over the same shared useAuth
 * — the request goes through the desktop transport, and the session lands in
 * the Go client's jar rather than a browser's cookie store. What the desktop
 * adds is the offer to be remembered: the web leaves a session cookie the
 * browser keeps, and the desktop has no browser to keep one, so staying signed
 * in between launches means putting the credentials in the operating system's
 * keychain — which is a choice, made here, not a default.
 */

definePageMeta({ layout: "auth" });
useHead({ title: "Sign in" });

const { signIn } = useAuth();
const router = useRouter();
const { $localeRoute: localeRoute } = useI18n();

const server = ref("");
const remember = ref(false);
const error = ref<string | null>(null);
const submitting = ref(false);

/*
 * What a sign-in needs before it is worth sending.
 *
 * Valibot rather than a hand-written check, so the rule is stated once and the
 * message beside the field is the same string the rule carries. The server
 * decides whether the credentials are right; this only decides whether there is
 * anything to ask about.
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

// Whether this machine has anywhere safe to keep credentials. Without a
// keychain the offer would be a checkbox that silently does nothing, so it is
// not shown; the servers screen already explains why signing in each time is
// what happens here.
const keychain = ref(false);

// The username last remembered for this server, offered so somebody is not
// retyping what the machine already knows.
onMounted(async () => {
  try {
    const current = await Api.Service.CurrentServer();
    if (!current) {
      // No server chosen yet — that screen comes first.
      await router.replace(localeRoute({ name: "servers" }));
      return;
    }
    server.value = current;

    try {
      keychain.value = (await Api.Service.CredentialStorage()) !== "none";
    } catch {
      keychain.value = false;
    }

    try {
      const remembered = await Api.Service.Recall(current);
      if (remembered) {
        form.setFieldValue("username", remembered);
        // A remembered username means they asked to be remembered before, so
        // the box starts ticked — unticking it is how somebody stops being.
        remember.value = true;
      }
    } catch {
      // Nothing remembered, or no keychain. The empty form is correct.
    }
  } catch (cause) {
    error.value = "Startup error: " + String((cause as Error)?.message ?? cause);
  }
});

/*
 * The form itself, which owns what was typed and when it is valid.
 *
 * The submit below is unchanged in what it does — it is the same sign-in, the
 * same offer to be remembered, the same messages. What it no longer does is
 * hold the two fields in refs of its own and decide by hand whether they are
 * filled in.
 */
const form = useForm({
  defaultValues: { username: "", password: "" },
  validators: { onSubmit: credentials },
  async onSubmit({ value }) {
    error.value = null;
    submitting.value = true;

    try {
      await signIn({ username: value.username, password: value.password });

      // Kept only if asked, and only where there is a keychain to keep it in.
      // The session token stays on the Go side; what is handed over is what the
      // person typed, so Go can sign in again with it after a relaunch.
      // Declining simply stores nothing — the server stays chosen and the
      // session stays live for this run; it is just not remembered past it.
      // Forgetting the server here would undo the sign-in that just succeeded.
      if (remember.value && keychain.value) {
        try {
          await Api.Service.Remember(server.value, value.username, value.password, "");
        } catch {
          // Being remembered failed, but signing in did not. Better to be
          // signed in and not remembered than to lose the sign-in over the
          // keychain.
        }
      }

      await router.push(localeRoute({ name: "index" }));
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
    <div class="mb-6 text-center">
      <h2 class="text-base font-semibold">Sign in</h2>
      <p v-if="server" class="mt-1 truncate text-xs text-muted-foreground">{{ server }}</p>
    </div>

    <form class="space-y-4" @submit.prevent="form.handleSubmit()">
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

      <label v-if="keychain" class="flex cursor-pointer items-center gap-2">
        <Checkbox v-model="remember" />
        <span class="text-sm">Keep me signed in on this machine</span>
      </label>

      <Button type="submit" class="h-9 w-full" :disabled="submitting">
        {{ submitting ? "Signing in" : "Sign in" }}
      </Button>
    </form>

    <p class="mt-6 text-center text-xs text-muted-foreground">
      Signing in to a different server?
      <NuxtLink
        :to="localeRoute({ name: 'servers' })"
        class="font-medium text-primary hover:underline"
      >
        Choose one
      </NuxtLink>
    </p>
  </div>
</template>
