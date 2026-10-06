<script setup lang="ts">
import { AtSign, LinkIcon } from "lucide-vue-next";

import { isApiError } from "@tdc/types";
import { Button } from "#layers/ui/app/components/ui/button";
import { Spinner } from "#layers/ui/app/components/ui/spinner";

/**
 * Where a confirmation link lands.
 *
 * Nothing to fill in: the token is what proves the address, so this confirms on
 * arrival and says what happened. Unauthenticated, because the link may be
 * opened in a mail client on a machine nobody is signed in on.
 */

definePageMeta({ layout: "auth" });
useHead({ title: "Confirm your address" });

const { completeConfirmation } = useRecovery();
const router = useRouter();
const route = useRoute();
const { $t, $ts, $localeRoute: localeRoute } = useI18n();

const working = ref(true);
const confirmed = ref(false);
const error = ref<string | null>(null);

onMounted(async () => {
  const token = typeof route.query.token === "string" ? route.query.token : "";
  if (!token) {
    working.value = false;
    error.value = "That link is missing its token.";
    return;
  }

  try {
    await completeConfirmation(token);
    confirmed.value = true;
  } catch (cause) {
    error.value = isApiError(cause)
      ? cause.message
      : "That link could not be used. Ask for another from your settings.";
  } finally {
    working.value = false;
  }
});
</script>

<template>
  <div>
    <p v-if="working" class="flex items-center gap-2 text-sm text-muted-foreground">
      <Spinner size="sm" />
      {{ $t("auth.confirmingAddress") }}
    </p>

    <template v-else-if="confirmed">
      <div class="flex gap-3">
        <span class="grid size-9 shrink-0 place-items-center rounded-lg bg-primary/12">
          <AtSign class="size-4 text-primary" aria-hidden="true" />
        </span>
        <div class="min-w-0">
          <h1 class="text-sm font-semibold">{{ $t("auth.addressConfirmed") }}</h1>
          <p class="mt-1 text-sm text-muted-foreground">
            {{ $t("auth.addressConfirmedDetail") }}
          </p>
        </div>
      </div>

      <Button
        class="mt-5 h-9 w-full"
        :label="$ts('action.continue')"
        @click="router.push(localeRoute({ name: 'index' }))"
      >
        {{ $t("action.continue") }}
      </Button>
    </template>

    <template v-else>
      <div class="flex gap-3">
        <span class="grid size-9 shrink-0 place-items-center rounded-lg bg-muted">
          <LinkIcon class="size-4 text-muted-foreground" aria-hidden="true" />
        </span>
        <div class="min-w-0">
          <h1 class="text-sm font-semibold">{{ $t("auth.linkCannotBeUsed") }}</h1>
          <p class="mt-1 text-sm text-muted-foreground">{{ error }}</p>
        </div>
      </div>

      <p class="mt-4 text-xs text-muted-foreground">
        {{ $t("auth.confirmationLinkExpiredDetail") }}
      </p>

      <Button
        variant="outline"
        class="mt-5 h-9 w-full"
        :label="$ts('auth.goToSignIn')"
        @click="router.push(localeRoute({ name: 'signin' }))"
      >
        {{ $t("auth.goToSignIn") }}
      </Button>
    </template>
  </div>
</template>
