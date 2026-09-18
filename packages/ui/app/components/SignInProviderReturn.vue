<script setup lang="ts">
import { useForm } from "@tanstack/vue-form";
import type { AnyFieldApi } from "@tanstack/vue-form";
import * as v from "valibot";
import { Link2, UserPlus } from "lucide-vue-next";

import type { OAuthResult } from "@tdc/types";
import { isApiError } from "@tdc/types";
import { Button } from "#layers/ui/app/components/ui/button";
import { Field } from "#layers/ui/app/components/ui/field";
import { Input } from "#layers/ui/app/components/ui/input";
import { Spinner } from "#layers/ui/app/components/ui/spinner";

const { $t, $ts } = useI18n();

/**
 * What a sign-in through a provider asks, when it asks anything.
 *
 * Two questions, and both exist because the answer is not the server's to
 * assume. A verified address matching an account is offered, not taken — the
 * provider proved control of an address, not of the account here that uses it,
 * so accepting means proving that account with its password. And a new account
 * is named by the person, since the provider's suggestion may be taken here or
 * simply not what they want to be called.
 */

const props = defineProps<{
  /**
   * The handle the callback left, for a client whose browser received the
   * redirect. Empty on the desktop, which never saw one.
   */
  handle?: string;
  /**
   * The state this sign-in began with, for a client that collects its own
   * outcome instead. Empty on the web, which has a handle.
   *
   * One or the other, never both: they name the same sign-in by two routes,
   * and the server refuses a request carrying both.
   */
  state?: string;
  /** What collecting it said, once. */
  result: OAuthResult;
}>();

/**
 * How this client names the sign-in it is finishing.
 *
 * Whichever it was given, so the three calls below say it once rather than
 * each deciding again.
 */
const claim = computed(() =>
  props.handle ? { handle: props.handle } : { state: props.state ?? "" },
);

const emit = defineEmits<{
  /** Signed in. The parent takes it from here. */
  done: [result: OAuthResult];
  /** Nothing came of it, and the sign-in form should return. */
  cancel: [];
}>();

const error = ref<string | null>(null);
const submitting = ref(false);

const { finish } = useOAuth();

/** The first thing wrong with a field, once somebody has touched it. */
function firstError(field: AnyFieldApi): string | undefined {
  if (!field.state.meta.isTouched) return undefined;
  const [issue] = field.state.meta.errors;
  if (!issue) return undefined;
  return typeof issue === "string" ? issue : (issue as { message?: string }).message;
}

/** Whatever went wrong, said the way the sign-in form says it. */
function report(cause: unknown) {
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
}

/*
 * Accepting a link needs the password of the account being linked to.
 *
 * Stated here as a rule of the form so the message beside the field is the
 * same string the rule carries, as everywhere else.
 */
const linkAnswer = v.object({
  password: v.pipe(v.string(), v.minLength(1, "Your password for that account goes here.")),
});

const linkForm = useForm({
  defaultValues: { password: "" },
  validators: { onSubmit: linkAnswer },
  async onSubmit({ value }) {
    error.value = null;
    submitting.value = true;
    try {
      emit("done", await finish({ ...claim.value, accept: true, password: value.password }));
    } catch (cause) {
      report(cause);
    } finally {
      submitting.value = false;
    }
  },
});

/*
 * Naming a new account.
 *
 * The same username rule the registration form states, so somebody is told
 * here rather than after a round trip.
 */
const newAccount = v.object({
  username: v.pipe(
    v.string(),
    v.trim(),
    v.minLength(1, "Pick a username."),
    v.regex(/^[a-z0-9_-]+$/i, "Letters, digits, hyphen and underscore only."),
  ),
  displayName: v.string(),
});

const accountForm = useForm({
  defaultValues: { username: "", displayName: "" },
  validators: { onSubmit: newAccount },
  async onSubmit({ value }) {
    error.value = null;
    submitting.value = true;
    try {
      emit(
        "done",
        await finish({
          ...claim.value,
          username: value.username,
          displayName: value.displayName,
        }),
      );
    } catch (cause) {
      report(cause);
    } finally {
      submitting.value = false;
    }
  },
});

/*
 * Declining a link is not a refusal to sign in.
 *
 * Somebody who wants a separate account from the one that happens to share
 * their address says so here, and gets one.
 */
async function declineLink() {
  error.value = null;
  submitting.value = true;
  try {
    emit("done", await finish({ ...claim.value, accept: false }));
  } catch (cause) {
    report(cause);
  } finally {
    submitting.value = false;
  }
}

/** What to say about an outcome that asked nothing and did not sign anybody in. */
const trouble = computed(() => {
  switch (props.result.status) {
    case "declined":
      return $t("auth.oauthDeclined");
    case "expired":
      return $t("auth.oauthExpired");
    case "already-attached":
      return $t("auth.oauthAlreadyAttached");
    case "failed":
      return $t("auth.oauthFailed");
    default:
      return null;
  }
});
</script>

<template>
  <div>
    <p
      v-if="error"
      class="mb-4 rounded-md bg-destructive/10 px-3 py-2 text-sm text-destructive"
      role="alert"
    >
      {{ error }}
    </p>

    <!-- A verified address matches an account here. -->
    <template v-if="props.result.status === 'link-offered'">
      <div class="flex gap-3">
        <span class="grid size-9 shrink-0 place-items-center rounded-lg bg-primary/12">
          <Link2 class="size-4 text-primary" aria-hidden="true" />
        </span>
        <div class="min-w-0">
          <h2 class="text-sm font-semibold">{{ $t("auth.connectToAccount") }}</h2>
          <p class="mt-1 text-sm text-muted-foreground">
            {{ $t("auth.connectToAccountHint") }}
          </p>
        </div>
      </div>

      <form class="mt-5 space-y-4" @submit.prevent="linkForm.handleSubmit()">
        <linkForm.Field name="password">
          <template #default="{ field }">
            <Field :label="$ts('auth.password')" :error="firstError(field)" required>
              <template #default="{ id, invalid, describedBy }">
                <Input
                  :id="id"
                  type="password"
                  :model-value="field.state.value"
                  autofocus
                  autocomplete="current-password"
                  :invalid="invalid"
                  :described-by="describedBy"
                  @update:model-value="field.handleChange($event)"
                  @blur="field.handleBlur"
                />
              </template>
            </Field>
          </template>
        </linkForm.Field>

        <Button class="h-9 w-full" type="submit" :disabled="submitting">
          <Spinner v-if="submitting" size="sm" />
          {{ submitting ? $t("auth.connecting") : $t("auth.connectAndSignIn") }}
        </Button>
      </form>

      <!-- Wanting a separate account is a real answer, not a failure to
           understand the question. -->
      <Button
        variant="ghost"
        class="mt-2 h-9 w-full"
        :label="$ts('auth.createSeparateAccount')"
        :disabled="submitting"
        @click="declineLink"
      >
        {{ $t("auth.createSeparateAccount") }}
      </Button>
    </template>

    <!-- Nothing matched, so there is an account to name. -->
    <template v-else-if="props.result.status === 'new-account'">
      <div class="flex gap-3">
        <span class="grid size-9 shrink-0 place-items-center rounded-lg bg-primary/12">
          <UserPlus class="size-4 text-primary" aria-hidden="true" />
        </span>
        <div class="min-w-0">
          <h2 class="text-sm font-semibold">{{ $t("auth.chooseUsername") }}</h2>
          <p class="mt-1 text-sm text-muted-foreground">
            {{ $t("auth.chooseUsernameHint") }}
          </p>
        </div>
      </div>

      <form class="mt-5 space-y-4" @submit.prevent="accountForm.handleSubmit()">
        <accountForm.Field name="username">
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
        </accountForm.Field>

        <accountForm.Field name="displayName">
          <template #default="{ field }">
            <Field :label="$ts('auth.displayName')" :error="firstError(field)">
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
        </accountForm.Field>

        <Button class="h-9 w-full" type="submit" :disabled="submitting">
          <Spinner v-if="submitting" size="sm" />
          {{ submitting ? $t("auth.creatingYourAccount") : $t("auth.createAccount") }}
        </Button>
      </form>
    </template>

    <!-- It did not happen, and saying which way is more use than saying so. -->
    <template v-else-if="trouble">
      <p class="rounded-md bg-muted px-3 py-2 text-sm text-muted-foreground" role="status">
        {{ trouble }}
      </p>
      <Button
        variant="outline"
        class="mt-4 h-9 w-full"
        :label="$ts('auth.backToSignIn')"
        @click="emit('cancel')"
      >
        {{ $t("auth.backToSignIn") }}
      </Button>
    </template>
  </div>
</template>
