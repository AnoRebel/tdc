<script setup lang="ts">
import { useForm } from "@tanstack/vue-form";
import type { AnyFieldApi } from "@tanstack/vue-form";
import * as v from "valibot";
import { Fingerprint, ShieldCheck } from "lucide-vue-next";

import { isApiError } from "@tdc/types";
import type { SecondFactorRequired, SecondFactorResult } from "@tdc/types";
import { Button } from "#layers/ui/app/components/ui/button";
import { Field } from "#layers/ui/app/components/ui/field";
import { Input } from "#layers/ui/app/components/ui/input";
import { Spinner } from "#layers/ui/app/components/ui/spinner";

/**
 * The second half of a sign-in.
 *
 * One field, taking either the app's six digits or a code off the printed list
 * — the server tries both rather than asking somebody in the middle of losing
 * their phone to say which they are using.
 */

const props = defineProps<{ pending: SecondFactorRequired }>();

const emit = defineEmits<{
  /** Signed in. The parent takes it from here. */
  done: [result: SecondFactorResult];
  /** Gave up, and the sign-in form should come back. */
  cancel: [];
}>();

const { completeSecondFactor } = useAuth();
const { answerSecondFactor, supported: passkeySupported } = usePasskeys();

/*
 * Whether a passkey can answer this.
 *
 * Both sides again: the account must have one, and this browser must be able to
 * use it. A button for something that cannot work is worse than no button.
 */
const passkeyOffered = computed(
  () => props.pending.methods?.passkey === true && passkeySupported.value,
);

/*
 * Whether a code can.
 *
 * An older server sends no methods at all, which is read as "a code", since
 * that was the only answer before passkeys could be one.
 */
const codeOffered = computed(() => props.pending.methods?.code !== false);

const usingPasskey = ref(false);

const { sendEmailedCode, submitEmailedCode } = useSecondFactor();

/*
 * Whether a code can be sent by mail.
 *
 * Offered last and only as a fallback, because it is the weakest of the three:
 * it is only as good as the mailbox. Somebody whose phone is in another room
 * reaches for it; somebody holding their phone should not.
 */
const emailOffered = computed(() => props.pending.methods?.email === true);
const emailSent = ref(false);
const sendingEmail = ref(false);

async function askForEmailedCode() {
  error.value = null;
  sendingEmail.value = true;

  try {
    await sendEmailedCode(props.pending.ticket);
    emailSent.value = true;
  } catch (cause) {
    error.value = isApiError(cause) ? cause.message : "Could not send a code.";
  } finally {
    sendingEmail.value = false;
  }
}

async function withPasskey() {
  error.value = null;
  usingPasskey.value = true;

  try {
    emit("done", await answerSecondFactor(props.pending.ticket));
  } catch (cause) {
    // Dismissing the authenticator's prompt is a decision, not a failure.
    if (
      cause instanceof DOMException &&
      (cause.name === "NotAllowedError" || cause.name === "AbortError")
    ) {
      return;
    }
    error.value = isApiError(cause) ? cause.message : "That passkey was not accepted.";
  } finally {
    usingPasskey.value = false;
  }
}

const error = ref<string | null>(null);
const submitting = ref(false);

/*
 * Something, rather than six digits.
 *
 * A recovery code is not six digits, and a rule that demanded them would refuse
 * the codes somebody reaches for precisely when nothing else is working.
 */
const answer = v.object({
  code: v.pipe(v.string(), v.trim(), v.minLength(1, "The code from your app, or a recovery code.")),
});

function firstError(field: AnyFieldApi): string | undefined {
  if (!field.state.meta.isTouched) return undefined;
  const [issue] = field.state.meta.errors;
  if (!issue) return undefined;
  return typeof issue === "string" ? issue : (issue as { message?: string }).message;
}

const form = useForm({
  defaultValues: { code: "" },
  validators: { onSubmit: answer },
  async onSubmit({ value }) {
    error.value = null;
    submitting.value = true;

    try {
      /*
       * Which route the code goes to depends on where it came from.
       *
       * An emailed code lives on the pending sign-in rather than in a
       * credential, so it is checked by its own route — sending it to the
       * authenticator app's would compare it against a secret this account may
       * not even have.
       */
      const result = emailSent.value
        ? await submitEmailedCode(props.pending.ticket, value.code)
        : await completeSecondFactor(props.pending.ticket, value.code);

      emit("done", result);
    } catch (cause) {
      if (isApiError(cause) && cause.code === "rate-limited") {
        // Past the allowance for this ticket. Said as what to do next, since
        // trying again here cannot work.
        error.value = "Too many codes have been tried. Sign in again to start over.";
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
    <div class="flex gap-3">
      <span class="grid size-9 shrink-0 place-items-center rounded-lg bg-primary/12">
        <ShieldCheck class="size-4 text-primary" aria-hidden="true" />
      </span>
      <div class="min-w-0">
        <h2 class="text-sm font-semibold">Enter your code</h2>
        <p class="mt-1 text-sm text-muted-foreground">
          From your authenticator app. If you cannot reach it, one of your recovery codes works here
          too.
        </p>
      </div>
    </div>

    <p
      v-if="error"
      class="mt-4 rounded-md bg-destructive/10 px-3 py-2 text-sm text-destructive"
      role="alert"
    >
      {{ error }}
    </p>

    <!-- The passkey first, where there is one: it is one gesture against six
         digits typed from another device. -->
    <Button
      v-if="passkeyOffered"
      type="button"
      class="mt-5 h-9 w-full justify-center gap-2"
      label="Use your passkey"
      :disabled="usingPasskey || submitting"
      @click="withPasskey"
    >
      <Spinner v-if="usingPasskey" size="sm" />
      <Fingerprint v-else class="size-4" aria-hidden="true" />
      {{ usingPasskey ? "Waiting for your device" : "Use your passkey" }}
    </Button>

    <div v-if="passkeyOffered && codeOffered" class="mt-5 flex items-center gap-3" role="separator">
      <span class="h-px flex-1 bg-border" />
      <span class="text-xs text-muted-foreground">or enter a code</span>
      <span class="h-px flex-1 bg-border" />
    </div>

    <form
      v-if="codeOffered || emailSent"
      class="mt-5 space-y-4"
      @submit.prevent="form.handleSubmit()"
    >
      <form.Field name="code">
        <template #default="{ field }">
          <Field label="Code" :error="firstError(field)" required>
            <template #default="{ id, invalid, describedBy }">
              <Input
                :id="id"
                :model-value="field.state.value"
                autofocus
                inputmode="text"
                autocomplete="one-time-code"
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
        {{ submitting ? "Checking" : "Continue" }}
      </Button>
    </form>

    <!-- The fallback, for somebody whose phone is somewhere else. Below the
         other two because it is the weakest, and a fallback offered first
         stops being a fallback. -->
    <p v-if="emailSent" class="mt-4 rounded-md bg-muted px-3 py-2 text-sm" role="status">
      A code is on its way to the address on your account. Enter it above — it works once, for five
      minutes.
    </p>
    <Button
      v-else-if="emailOffered"
      variant="ghost"
      class="mt-2 h-9 w-full text-muted-foreground"
      label="Send a code to my email instead"
      :disabled="sendingEmail || submitting || usingPasskey"
      @click="askForEmailedCode"
    >
      <Spinner v-if="sendingEmail" size="sm" />
      Send a code to my email instead
    </Button>

    <!-- Said only when it is nearly true, so it stays worth reading. -->
    <p
      v-if="props.pending.recoveryCodesLeft > 0 && props.pending.recoveryCodesLeft <= 3"
      class="mt-4 text-xs text-muted-foreground"
    >
      You have {{ props.pending.recoveryCodesLeft }} recovery
      {{ props.pending.recoveryCodesLeft === 1 ? "code" : "codes" }} left. Make more from your
      settings once you are in.
    </p>

    <Button
      variant="ghost"
      class="mt-2 h-9 w-full"
      label="Back to sign in"
      :disabled="submitting"
      @click="emit('cancel')"
    >
      Back to sign in
    </Button>
  </div>
</template>
