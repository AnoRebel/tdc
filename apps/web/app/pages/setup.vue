<script setup lang="ts">
import { useForm } from "@tanstack/vue-form";
import type { AnyFieldApi } from "@tanstack/vue-form";
import * as v from "valibot";

import { Field } from "#layers/ui/app/components/ui/field";
import { Input } from "#layers/ui/app/components/ui/input";
import { isApiError } from "@tdc/types";
import { ShieldCheck } from "lucide-vue-next";
import { Spinner } from "#layers/ui/app/components/ui/spinner";
import { Button } from "#layers/ui/app/components/ui/button";

definePageMeta({ layout: "auth" });

useHead({ title: "Set up this server" });

/**
 * First-run setup: creates the server's first administrator.
 *
 * Reachable only while the server has no accounts. The server refuses once one
 * exists, so this page cannot be used later to mint an administrator; the
 * redirect below is a courtesy, not the control.
 */

const { setupStatus, completeSetup } = useAuth();
const router = useRouter();
const { $localeRoute: localeRoute } = useI18n();

const error = ref<string | null>(null);
const submitting = ref(false);

/*
 * The first administrator, which is the one account nobody can be given.
 *
 * The same rules registration uses, because this is a registration — the only
 * difference is that it happens once and closes behind itself.
 */
const administrator = v.object({
  username: v.pipe(
    v.string(),
    v.trim(),
    v.minLength(1, "Choose a username."),
    v.regex(/^[a-z0-9_.-]+$/i, "Letters, digits, underscore, dot and hyphen."),
  ),
  displayName: v.string(),
  password: v.pipe(v.string(), v.minLength(12, "At least twelve characters.")),
});

/** The first thing wrong with a field, once somebody has touched it. */
function firstError(field: AnyFieldApi): string | undefined {
  if (!field.state.meta.isTouched) return undefined;
  const [issue] = field.state.meta.errors;
  if (!issue) return undefined;
  return typeof issue === "string" ? issue : (issue as { message?: string }).message;
}

const status = await setupStatus();
if (!status.setupRequired) {
  await router.replace(localeRoute({ name: "signin" }));
}

const form = useForm({
  defaultValues: { username: "", displayName: "", password: "" },
  validators: { onSubmit: administrator },
  async onSubmit({ value }) {
    error.value = null;
    submitting.value = true;

    try {
      await completeSetup({
        username: value.username,
        password: value.password,
        displayName: value.displayName || undefined,
      });
      await router.push(localeRoute({ name: "index" }));
    } catch (cause) {
      error.value = isApiError(cause) ? cause.message : "Could not reach the server.";
    } finally {
      submitting.value = false;
    }
  },
});
</script>

<template>
  <div>
    <span class="grid size-9 place-items-center rounded-lg bg-primary/12">
      <ShieldCheck class="size-4 text-primary" />
    </span>

    <h1 class="mt-4 text-xl font-semibold">Set up this server</h1>
    <p class="mt-1 text-sm text-muted-foreground">
      This account becomes the server's administrator. It can only be created once.
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
          <Field label="Username" :error="firstError(field)" required>
            <template #default="{ id, invalid, describedBy }">
              <Input
                :id="id"
                autocomplete="username"
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

      <form.Field name="displayName">
        <template #default="{ field }">
          <Field label="Display name" hint="Optional." :error="firstError(field)">
            <template #default="{ id, invalid, describedBy }">
              <Input
                :id="id"
                autocomplete="name"
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

      <form.Field name="password">
        <template #default="{ field }">
          <Field
            label="Password"
            hint="At least twelve characters."
            :error="firstError(field)"
            required
          >
            <template #default="{ id, invalid, describedBy }">
              <Input
                :id="id"
                type="password"
                autocomplete="new-password"
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

      <Button class="h-9 w-full" type="submit" :disabled="submitting">
        <Spinner v-if="submitting" size="sm" />
        {{ submitting ? "Creating administrator" : "Create administrator" }}
      </Button>
    </form>
  </div>
</template>
