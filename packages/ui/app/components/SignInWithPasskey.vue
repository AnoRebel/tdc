<script setup lang="ts">
import { Fingerprint } from "lucide-vue-next";
import { Button } from "#layers/ui/app/components/ui/button";
import { Spinner } from "#layers/ui/app/components/ui/spinner";

import { isApiError } from "@tdc/types";
import type { SignInResult } from "@tdc/types";

const { $t, $ts } = useI18n();

/**
 * The button for signing in with a passkey.
 *
 * Draws nothing unless both sides can do it — the server must have a canonical
 * address to bind passkeys to, and the browser must have the credentials API —
 * so both sign-in forms can include this unconditionally.
 *
 * Above the password fields rather than below them, because for somebody who
 * has a passkey this is the whole sign-in and the form beneath is the fallback.
 */

const emit = defineEmits<{
  /** Signed in. The parent takes it from here. */
  done: [result: SignInResult];
}>();

const { available, signIn } = usePasskeys();

const busy = ref(false);
const error = ref<string | null>(null);

async function attempt() {
  error.value = null;
  busy.value = true;

  try {
    emit("done", await signIn());
  } catch (cause) {
    /*
     * Dismissing the authenticator's prompt is not a failure.
     *
     * The browser reports it as an abort, and it is what happens when somebody
     * changes their mind or picks the wrong authenticator. Saying "sign-in
     * failed" for a thing they deliberately cancelled would be a lie they have
     * to read every time.
     */
    if (
      cause instanceof DOMException &&
      (cause.name === "NotAllowedError" || cause.name === "AbortError")
    ) {
      return;
    }

    if (isApiError(cause) && cause.code === "rate-limited") {
      const wait = cause.retryAfterSeconds;
      error.value = wait
        ? $ts("auth.tooManyAttemptsMinutes", { count: Math.ceil(wait / 60) })
        : $ts("auth.tooManyAttempts");
    } else if (isApiError(cause)) {
      error.value = cause.message;
    } else {
      error.value = $ts("auth.passkeyDidNotSignIn");
    }
  } finally {
    busy.value = false;
  }
}
</script>

<template>
  <div v-if="available" class="mt-6">
    <p
      v-if="error"
      class="mb-3 rounded-md bg-destructive/10 px-3 py-2 text-sm text-destructive"
      role="alert"
    >
      {{ error }}
    </p>

    <Button
      type="button"
      class="h-9 w-full justify-center gap-2"
      :label="$ts('auth.signInWithPasskey')"
      :disabled="busy"
      @click="attempt"
    >
      <Spinner v-if="busy" size="sm" />
      <Fingerprint v-else class="size-4" aria-hidden="true" />
      {{ busy ? $t("auth.waitingForDevice") : $t("auth.signInWithPasskey") }}
    </Button>

    <!-- What the fields below are, rather than an unlabelled rule. -->
    <div class="mt-6 flex items-center gap-3" role="separator">
      <span class="h-px flex-1 bg-border" />
      <span class="text-xs text-muted-foreground">{{ $t("auth.orUsePassword") }}</span>
      <span class="h-px flex-1 bg-border" />
    </div>
  </div>
</template>
