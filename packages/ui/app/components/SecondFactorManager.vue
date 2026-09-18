<script setup lang="ts">
import { useForm } from "@tanstack/vue-form";
import type { AnyFieldApi } from "@tanstack/vue-form";
import * as v from "valibot";
import { Check, Copy, ShieldCheck, ShieldOff } from "lucide-vue-next";
import { Button } from "#layers/ui/app/components/ui/button";
import { Field } from "#layers/ui/app/components/ui/field";
import { Input } from "#layers/ui/app/components/ui/input";
import { Spinner } from "#layers/ui/app/components/ui/spinner";

import { isApiError } from "@tdc/types";
import type { SecondFactorStatus } from "@tdc/types";

const { $t, $ts } = useI18n();

/**
 * Somebody's own second factor: setting one up, replacing the recovery codes,
 * and turning it off.
 *
 * Enrolment is two steps on purpose. Between the secret being issued and a code
 * proving the app holds it, nothing is required at sign-in — so a scan that
 * silently failed cannot lock somebody out of their own account.
 */

const { status, begin, confirm, reissueRecoveryCodes, disable } = useSecondFactor();

const state = ref<SecondFactorStatus | null>(null);
const loading = ref(false);
const busy = ref(false);
const error = ref<string | null>(null);

// The half-finished enrolment, held only until it is confirmed.
const setup = ref<{ secret: string; url: string } | null>(null);

/*
 * The recovery codes, shown once.
 *
 * Only their hashes are stored, so this is genuinely the only moment they
 * exist anywhere but on whatever somebody writes them onto.
 */
const codes = ref<string[] | null>(null);
const copied = ref(false);

const disabling = ref(false);

/** The first thing wrong with a field, once somebody has touched it. */
function firstError(field: AnyFieldApi): string | undefined {
  if (!field.state.meta.isTouched) return undefined;
  const [issue] = field.state.meta.errors;
  if (!issue) return undefined;
  return typeof issue === "string" ? issue : (issue as { message?: string }).message;
}

async function load() {
  loading.value = true;
  try {
    state.value = await status();
  } catch (cause) {
    error.value = isApiError(cause) ? cause.message : "Could not read your settings.";
  } finally {
    loading.value = false;
  }
}

async function startSetup() {
  error.value = null;
  busy.value = true;
  try {
    setup.value = await begin();
  } catch (cause) {
    error.value = isApiError(cause) ? cause.message : "Could not start setting this up.";
  } finally {
    busy.value = false;
  }
}

/*
 * Confirming the app holds the secret.
 *
 * Six digits, stated as a rule so the message beside the field is the string
 * the rule carries — the same shape every other form here uses.
 */
const confirmForm = useForm({
  defaultValues: { code: "" },
  validators: {
    onSubmit: v.object({
      code: v.pipe(
        v.string(),
        v.trim(),
        v.regex(/^\d{6}$/, "Six digits, from your authenticator app."),
      ),
    }),
  },
  async onSubmit({ value }) {
    error.value = null;
    busy.value = true;
    try {
      codes.value = await confirm(value.code);
      setup.value = null;
      confirmForm.reset();
      await load();
    } catch (cause) {
      error.value = isApiError(cause) ? cause.message : "That code was not accepted.";
    } finally {
      busy.value = false;
    }
  },
});

async function makeNewCodes() {
  error.value = null;
  busy.value = true;
  try {
    codes.value = await reissueRecoveryCodes();
    await load();
  } catch (cause) {
    error.value = isApiError(cause) ? cause.message : "Could not make new codes.";
  } finally {
    busy.value = false;
  }
}

// Turning it off, which needs the password rather than just a session.
const disableForm = useForm({
  defaultValues: { password: "" },
  validators: {
    onSubmit: v.object({
      password: v.pipe(v.string(), v.minLength(1, "Your password goes here.")),
    }),
  },
  async onSubmit({ value }) {
    error.value = null;
    busy.value = true;
    try {
      await disable(value.password);
      disableForm.reset();
      disabling.value = false;
      await load();
    } catch (cause) {
      error.value = isApiError(cause) ? cause.message : "Could not turn this off.";
    } finally {
      busy.value = false;
    }
  },
});

/** Copying the codes, since typing ten of them out is how one gets mistyped. */
async function copyCodes() {
  if (!codes.value) return;
  try {
    await navigator.clipboard.writeText(codes.value.join("\n"));
    copied.value = true;
    setTimeout(() => (copied.value = false), 2000);
  } catch {
    // No clipboard, or permission refused. They are on screen to be written
    // down either way, which is what they are for.
  }
}

onMounted(load);
</script>

<template>
  <section class="space-y-4">
    <div>
      <h2 class="flex items-center gap-1.5 text-sm font-semibold">
        <ShieldCheck class="size-4" />
        {{ $t("auth.twoStep") }}
      </h2>
      <p class="mt-1 text-xs text-muted-foreground">
        {{ $t("auth.twoStepHint") }}
      </p>
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
      {{ $t("auth.readingSettings") }}
    </p>

    <template v-else-if="state">
      <!-- Why they are being asked, where the operator is the reason. -->
      <p
        v-if="state.required && !state.enrolled"
        class="rounded-md bg-primary/10 px-3 py-2 text-sm"
        role="status"
      >
        {{ $t("auth.twoStepRequired") }}
      </p>

      <!-- The codes, at the one moment they exist. -->
      <div v-if="codes" class="rounded-lg border p-4">
        <h3 class="text-sm font-semibold">{{ $t("auth.recoveryCodesTitle") }}</h3>
        <p class="mt-1 text-xs text-muted-foreground">
          {{ $t("auth.recoveryCodesHint") }}
        </p>

        <ul class="mt-3 grid grid-cols-2 gap-1 font-mono text-sm">
          <li v-for="recovery in codes" :key="recovery">{{ recovery }}</li>
        </ul>

        <div class="mt-3 flex gap-2">
          <Button
            variant="outline"
            class="h-8 gap-1.5"
            :label="$ts('auth.copyRecoveryCodes')"
            @click="copyCodes"
          >
            <Check v-if="copied" class="size-3.5" aria-hidden="true" />
            <Copy v-else class="size-3.5" aria-hidden="true" />
            {{ copied ? $t("auth.copied") : $t("action.copy") }}
          </Button>
          <Button
            variant="ghost"
            class="h-8"
            :label="$ts('auth.keptTheseSafe')"
            @click="codes = null"
          >
            {{ $t("auth.keptTheseSafe") }}
          </Button>
        </div>
      </div>

      <!-- Setting up: the secret, then a code proving the app has it. -->
      <div v-else-if="setup" class="rounded-lg border p-4">
        <h3 class="text-sm font-semibold">{{ $t("auth.addToAuthenticator") }}</h3>
        <p class="mt-1 text-xs text-muted-foreground">
          {{ $t("auth.addToAuthenticatorHint") }}
        </p>

        <p class="mt-3 font-mono text-sm break-all select-all">{{ setup.secret }}</p>

        <form class="mt-4 space-y-3" @submit.prevent="confirmForm.handleSubmit()">
          <confirmForm.Field name="code">
            <template #default="{ field }">
              <Field :label="$ts('auth.codeFromApp')" :error="firstError(field)" required>
                <template #default="{ id, invalid, describedBy }">
                  <Input
                    :id="id"
                    :model-value="field.state.value"
                    autofocus
                    inputmode="numeric"
                    autocomplete="one-time-code"
                    :invalid="invalid"
                    :described-by="describedBy"
                    @update:model-value="field.handleChange($event)"
                    @blur="field.handleBlur"
                  />
                </template>
              </Field>
            </template>
          </confirmForm.Field>

          <div class="flex gap-2">
            <Button type="submit" class="h-9" :disabled="busy">
              <Spinner v-if="busy" size="sm" />
              {{ busy ? $t("auth.checking") : $t("action.confirm") }}
            </Button>
            <Button
              variant="ghost"
              class="h-9"
              :label="$ts('auth.cancelSettingUp')"
              @click="setup = null"
            >
              {{ $t("action.cancel") }}
            </Button>
          </div>
        </form>
      </div>

      <!-- A passkey is already doing this job, so say so rather than letting
           somebody set up an app they did not need. -->
      <p
        v-if="state.methods?.passkey && !state.methods?.code"
        class="rounded-md bg-muted px-3 py-2 text-sm"
        role="status"
      >
        {{ $t("auth.passkeyAlreadyCovers") }}
      </p>

      <!-- Already on. -->
      <template v-else-if="state.enrolled">
        <p class="text-sm">
          {{
            state.recoveryCodesLeft === 1
              ? $t("auth.onWithCodeLeft", { count: state.recoveryCodesLeft })
              : $t("auth.onWithCodesLeft", { count: state.recoveryCodesLeft })
          }}
        </p>

        <div class="flex flex-wrap gap-2">
          <Button
            variant="outline"
            class="h-9"
            :disabled="busy"
            :label="$ts('auth.makeNewRecoveryCodes')"
            @click="makeNewCodes"
          >
            {{ $t("auth.makeNewRecoveryCodes") }}
          </Button>

          <!-- Not offered where the operator requires it, since it would be a
               button that can only refuse. -->
          <Button
            v-if="!state.required && !disabling"
            variant="ghost"
            class="h-9 gap-1.5 text-muted-foreground hover:text-destructive"
            :label="$ts('auth.turnOffTwoStep')"
            @click="disabling = true"
          >
            <ShieldOff class="size-3.5" aria-hidden="true" />
            {{ $t("auth.turnOff") }}
          </Button>
        </div>

        <form
          v-if="disabling"
          class="space-y-3 rounded-lg border p-4"
          @submit.prevent="disableForm.handleSubmit()"
        >
          <p class="text-sm text-muted-foreground">
            {{ $t("auth.turnOffPasswordHint") }}
          </p>

          <disableForm.Field name="password">
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
          </disableForm.Field>

          <div class="flex gap-2">
            <Button
              type="submit"
              class="h-9 bg-destructive text-destructive-foreground hover:opacity-90"
              :disabled="busy"
            >
              <Spinner v-if="busy" size="sm" />
              {{ busy ? $t("auth.turningOff") : $t("auth.turnOff") }}
            </Button>
            <Button
              variant="ghost"
              class="h-9"
              :label="$ts('auth.keepTwoStep')"
              @click="disabling = false"
            >
              {{ $t("auth.keepItOn") }}
            </Button>
          </div>
        </form>
      </template>

      <!-- Off, and offered. -->
      <Button
        v-else
        class="h-9"
        :disabled="busy"
        :label="$ts('auth.setUpTwoStep')"
        @click="startSetup"
      >
        <Spinner v-if="busy" size="sm" />
        {{ $t("auth.setItUp") }}
      </Button>
    </template>
  </section>
</template>
