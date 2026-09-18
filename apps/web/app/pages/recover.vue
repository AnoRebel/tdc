<script setup lang="ts">
import { useForm } from "@tanstack/vue-form";
import type { AnyFieldApi } from "@tanstack/vue-form";
import * as v from "valibot";
import { KeyRound, LinkIcon } from "lucide-vue-next";

import { isApiError } from "@tdc/types";
import { Button } from "#layers/ui/app/components/ui/button";
import { Field } from "#layers/ui/app/components/ui/field";
import { Input } from "#layers/ui/app/components/ui/input";
import { Spinner } from "#layers/ui/app/components/ui/spinner";

/**
 * Where a recovery link lands: choose a new password.
 *
 * The link is checked before the form is drawn, so somebody following an
 * expired one is told now rather than after they have chosen a password twice.
 */

definePageMeta({ layout: "auth" });
useHead({ title: "Set a new password" });

const { usable, redeem } = useRecovery();
const router = useRouter();
const route = useRoute();
const { $t, $ts, $localeRoute: localeRoute } = useI18n();

const token = computed(() => {
  const value = route.query.token;
  return typeof value === "string" ? value : "";
});

const checking = ref(true);
const linkWorks = ref(false);
const done = ref(false);
const error = ref<string | null>(null);
const submitting = ref(false);

/*
 * Two fields, because a password nobody can see is a password easily mistyped
 * — and this one cannot be recovered a second time without another link.
 */
const chosen = v.pipe(
  v.object({
    password: v.pipe(
      v.string(),
      // The server has the real rule and will say so; this is the one that
      // saves a round trip for the commonest mistake.
      v.minLength(12, "Twelve characters or more."),
    ),
    again: v.string(),
  }),
  v.forward(
    v.partialCheck(
      [["password"], ["again"]],
      (input) => input.password === input.again,
      "Those do not match.",
    ),
    ["again"],
  ),
);

function firstError(field: AnyFieldApi): string | undefined {
  if (!field.state.meta.isTouched) return undefined;
  const [issue] = field.state.meta.errors;
  if (!issue) return undefined;
  return typeof issue === "string" ? issue : (issue as { message?: string }).message;
}

const form = useForm({
  defaultValues: { password: "", again: "" },
  validators: { onSubmit: chosen },
  async onSubmit({ value }) {
    error.value = null;
    submitting.value = true;

    try {
      await redeem(token.value, value.password);
      done.value = true;
    } catch (cause) {
      if (isApiError(cause)) {
        error.value = cause.message;
        // A spent or expired link cannot be retried, so the form goes away
        // rather than inviting a second attempt that cannot work.
        if (cause.status === 404) linkWorks.value = false;
      } else {
        error.value = "Could not reach the server.";
      }
    } finally {
      submitting.value = false;
    }
  },
});

onMounted(async () => {
  if (!token.value) {
    checking.value = false;
    return;
  }
  linkWorks.value = await usable(token.value);
  checking.value = false;
});
</script>

<template>
  <div>
    <p v-if="checking" class="flex items-center gap-2 text-sm text-muted-foreground">
      <Spinner size="sm" />
      {{ $t("auth.checkingLink") }}
    </p>

    <!-- Done. Deliberately not signed in: signing in is what proves the
         password is known, and goes through the second factor where there is
         one. -->
    <template v-else-if="done">
      <div class="flex gap-3">
        <span class="grid size-9 shrink-0 place-items-center rounded-lg bg-primary/12">
          <KeyRound class="size-4 text-primary" aria-hidden="true" />
        </span>
        <div class="min-w-0">
          <h1 class="text-sm font-semibold">{{ $t("auth.passwordChanged") }}</h1>
          <p class="mt-1 text-sm text-muted-foreground">
            {{ $t("auth.passwordChangedDetail") }}
          </p>
        </div>
      </div>

      <Button
        class="mt-5 h-9 w-full"
        :label="$ts('auth.goToSignIn')"
        @click="router.push(localeRoute({ name: 'signin' }))"
      >
        {{ $t("auth.signIn") }}
      </Button>
    </template>

    <!-- No usable link. -->
    <template v-else-if="!linkWorks">
      <div class="flex gap-3">
        <span class="grid size-9 shrink-0 place-items-center rounded-lg bg-muted">
          <LinkIcon class="size-4 text-muted-foreground" aria-hidden="true" />
        </span>
        <div class="min-w-0">
          <h1 class="text-sm font-semibold">{{ $t("auth.linkCannotBeUsed") }}</h1>
          <p class="mt-1 text-sm text-muted-foreground">
            {{ $t("auth.recoveryLinkExpiredDetail") }}
          </p>
        </div>
      </div>

      <Button
        variant="outline"
        class="mt-5 h-9 w-full"
        :label="$ts('auth.backToSignIn')"
        @click="router.push(localeRoute({ name: 'signin' }))"
      >
        {{ $t("auth.backToSignIn") }}
      </Button>
    </template>

    <template v-else>
      <h1 class="text-sm font-semibold">{{ $t("auth.setNewPassword") }}</h1>
      <p class="mt-1 text-sm text-muted-foreground">
        {{ $t("auth.setNewPasswordDetail") }}
      </p>

      <p
        v-if="error"
        class="mt-4 rounded-md bg-destructive/10 px-3 py-2 text-sm text-destructive"
        role="alert"
      >
        {{ error }}
      </p>

      <form class="mt-5 space-y-4" @submit.prevent="form.handleSubmit()">
        <form.Field name="password">
          <template #default="{ field }">
            <Field :label="$ts('auth.newPassword')" :error="firstError(field)" required>
              <template #default="{ id, invalid, describedBy }">
                <Input
                  :id="id"
                  type="password"
                  :model-value="field.state.value"
                  autofocus
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

        <form.Field name="again">
          <template #default="{ field }">
            <Field :label="$ts('auth.again')" :error="firstError(field)" required>
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
          {{ submitting ? $t("auth.settingPassword") : $t("auth.setThePassword") }}
        </Button>
      </form>
    </template>
  </div>
</template>
