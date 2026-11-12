<script setup lang="ts">
import { useForm } from "@tanstack/vue-form";
import type { AnyFieldApi } from "@tanstack/vue-form";
import * as v from "valibot";

import type { OAuthResult, SecondFactorRequired } from "@tdc/types";
import { isApiError, needsSecondFactor } from "@tdc/types";
import { Button } from "#layers/ui/app/components/ui/button";
import { Field } from "#layers/ui/app/components/ui/field";
import { Input } from "#layers/ui/app/components/ui/input";
import { Spinner } from "#layers/ui/app/components/ui/spinner";

definePageMeta({ layout: "auth" });

const { signIn, setupStatus, refresh } = useAuth();
const router = useRouter();
const { $t, $ts, $localeRoute: localeRoute } = useI18n();
useHead({ title: $ts("auth.signIn") });

const error = ref<string | null>(null);
const submitting = ref(false);

const { begin, pendingHandle, finish } = useOAuth();
const { available: recoveryAvailable } = useRecovery();

/*
 * Signing in through a provider, which has two moments on this page.
 *
 * Leaving: a button hands the browser to the provider. Returning: the callback
 * sends it back here with a handle in the fragment, and what that handle stands
 * for may be a session, or a question.
 */
/*
 * A sign-in that stopped short of a session.
 *
 * Set when the password was right and the account owes a second factor. The
 * form is replaced rather than supplemented: there is one thing to do, and
 * offering two is how somebody answers the wrong one.
 */
const awaitingSecondFactor = ref<SecondFactorRequired | null>(null);

// Asking for a way back in, shown in place of the form rather than beside it.
const askingForRecovery = ref(false);

const openingProvider = ref<string | null>(null);
const returned = ref<OAuthResult | null>(null);
const returnedHandle = ref("");

/** Where they were trying to reach before any of this started. */
function intendedNext(): string | undefined {
  const next = router.currentRoute.value.query.next;
  return typeof next === "string" ? next : undefined;
}

async function chooseProvider(provider: string) {
  error.value = null;
  openingProvider.value = provider;
  try {
    const start = await begin(provider, intendedNext());
    // Replacing rather than pushing: coming back from the provider should not
    // land somebody on this page twice in their history.
    window.location.replace(start.url);
  } catch (cause) {
    openingProvider.value = null;
    error.value = isApiError(cause) ? cause.message : $ts("error.couldNotReach");
  }
}

/** Somebody finished a sign-in that owed a second factor. */
async function signedInWithSecondFactor() {
  awaitingSecondFactor.value = null;
  await router.push(intendedNext() || localeRoute({ name: "index" }));
}

/** Somebody signed in with a passkey, which is already a session. */
async function signedInWithPasskey() {
  await refresh();
  await router.push(intendedNext() || localeRoute({ name: "index" }));
}

/** Somebody signed in through a provider, one way or another. */
async function signedInThroughProvider(result: OAuthResult) {
  returned.value = null;
  // The cookie is already set; this is what tells the rest of the application
  // who is here.
  await refresh();
  await router.push(result.redirectTo || intendedNext() || localeRoute({ name: "index" }));
}

/*
 * What a sign-in needs before it is worth sending.
 *
 * The same schema the desktop client uses, for the same reason: the rule is
 * stated once and the message beside a field is the string the rule carries.
 * Whether the credentials are right is the server's to say; this only decides
 * whether there is anything to ask about.
 */
const credentials = v.object({
  username: v.pipe(v.string(), v.trim(), v.minLength(1, $ts("auth.usernameGoesHere"))),
  password: v.pipe(v.string(), v.minLength(1, $ts("auth.passwordGoesHere"))),
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

/*
 * Returning from a provider.
 *
 * Collected here rather than on a page of its own, because the answer may be a
 * question and the place to ask it is where somebody was already signing in.
 * The handle is spent in the reading, so a reload shows the form rather than
 * failing to collect something that has already been collected.
 */
const handle = pendingHandle();
if (handle) {
  try {
    const result = await finish({ handle });
    if (result.status === "signed-in" || result.status === "linked") {
      await signedInThroughProvider(result);
    } else {
      returnedHandle.value = handle;
      returned.value = result;
    }
  } catch (cause) {
    error.value = isApiError(cause) ? cause.message : $ts("auth.couldNotCompleteSignIn");
  }
}

const form = useForm({
  defaultValues: { username: "", password: "" },
  validators: { onSubmit: credentials },
  async onSubmit({ value }) {
    error.value = null;
    submitting.value = true;

    try {
      const outcome = await signIn({ username: value.username, password: value.password });

      if (needsSecondFactor(outcome)) {
        awaitingSecondFactor.value = outcome;
        return;
      }

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
          ? $ts("auth.tooManyAttemptsMinutes", { count: Math.ceil(wait / 60) })
          : $ts("auth.tooManyAttempts");
      } else if (isApiError(cause)) {
        error.value = cause.message;
      } else {
        error.value = $ts("error.couldNotReach");
      }
    } finally {
      submitting.value = false;
    }
  },
});
</script>

<template>
  <div>
    <!-- Returning from a provider with a question to answer. The sign-in form
         is not shown underneath it: there is one thing to do here, and offering
         two is how somebody answers the wrong one. -->
    <template v-if="returned">
      <SignInProviderReturn
        :handle="returnedHandle"
        :result="returned"
        @done="signedInThroughProvider"
        @cancel="returned = null"
      />
    </template>

    <RecoveryRequest v-else-if="askingForRecovery" @cancel="askingForRecovery = false" />

    <!-- The password was right, and something more is needed. -->
    <SecondFactorPrompt
      v-else-if="awaitingSecondFactor"
      :pending="awaitingSecondFactor"
      @done="signedInWithSecondFactor"
      @cancel="awaitingSecondFactor = null"
    />

    <template v-else>
      <h1 class="text-xl font-semibold">{{ $t("auth.signIn") }}</h1>
      <p class="mt-1 text-sm text-muted-foreground">{{ $t("auth.welcomeBack") }}</p>

      <!-- Above the password fields, because for somebody who has a passkey
           this is the whole sign-in and the form beneath is the fallback.
           Draws nothing where either side cannot do it. -->
      <SignInWithPasskey @done="signedInWithPasskey" />

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
            <Field :label="$ts('auth.usernameOrEmail')" :error="firstError(field)" required>
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
            <Field :label="$ts('auth.password')" :error="firstError(field)" required>
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
          {{ submitting ? $t("auth.signingIn") : $t("auth.signIn") }}
        </Button>
      </form>

      <!-- Draws nothing when the server offers no providers, which is most of
           them, so this needs no condition around it here. -->
      <SignInProviders :busy="openingProvider" @choose="chooseProvider" />

      <!-- Only where the server can send. A link that leads to a form which
           answers with a refusal is worse than no link. -->
      <p v-if="recoveryAvailable" class="mt-4 text-center text-sm">
        <button
          type="button"
          class="font-medium text-muted-foreground hover:text-foreground hover:underline"
          @click="askingForRecovery = true"
        >
          {{ $t("auth.forgottenPassword") }}
        </button>
      </p>

      <p class="mt-6 text-center text-sm text-muted-foreground">
        {{ $t("auth.noAccountYet") }}
        <NuxtLink
          :to="localeRoute({ name: 'register' })"
          class="font-medium text-primary hover:underline"
        >
          {{ $t("auth.createOne") }}
        </NuxtLink>
      </p>
    </template>
  </div>
</template>
