<script setup lang="ts">
import type { Component } from "vue";
import { Github, Gitlab, KeyRound, LogIn } from "lucide-vue-next";
import { Button } from "#layers/ui/app/components/ui/button";
import { Spinner } from "#layers/ui/app/components/ui/spinner";

const { $t, $ts } = useI18n();

/**
 * The buttons for signing in through a provider somebody else runs.
 *
 * Draws nothing when the server offers none, which is the ordinary case — so
 * both sign-in forms can include this unconditionally rather than each deciding
 * whether to.
 *
 * Opening the browser is the parent's job. The web navigates in place; the
 * desktop hands the URL to the system browser, because a sign-in inside an
 * application window is where somebody cannot see the address they are typing
 * their password into.
 */

const props = defineProps<{
  /** Where to return to afterwards, as a path inside this application. */
  next?: string;
  /** Set while the parent is opening one, so the buttons can show it. */
  busy?: string | null;
}>();

const emit = defineEmits<{ choose: [provider: string] }>();

const { providers, offered } = useOAuth();

/*
 * An icon for the providers people recognise, and a generic one otherwise.
 *
 * Matched on the configured name, which the operator chooses, so this is a
 * convenience rather than something to depend on: a provider called anything
 * else gets the generic icon and reads perfectly well.
 */
const icons: Record<string, Component> = {
  github: Github,
  gitlab: Gitlab,
  keycloak: KeyRound,
};

function iconFor(name: string) {
  return icons[name.toLowerCase()] ?? LogIn;
}
</script>

<template>
  <div v-if="offered" class="mt-6">
    <!-- A divider that says what the buttons below are, rather than an
         unlabelled rule: "or" alone leaves somebody to infer it. -->
    <div class="flex items-center gap-3" role="separator">
      <span class="h-px flex-1 bg-border" />
      <span class="text-xs text-muted-foreground">{{ $t("auth.orContinueWith") }}</span>
      <span class="h-px flex-1 bg-border" />
    </div>

    <div class="mt-4 grid gap-2">
      <Button
        v-for="provider in providers"
        :key="provider.name"
        type="button"
        variant="outline"
        class="h-9 w-full justify-center gap-2"
        :label="$ts('auth.continueWith', { provider: provider.label })"
        :disabled="Boolean(props.busy)"
        @click="emit('choose', provider.name)"
      >
        <Spinner v-if="props.busy === provider.name" size="sm" />
        <component :is="iconFor(provider.name)" v-else class="size-4" aria-hidden="true" />
        {{ provider.label }}
      </Button>
    </div>
  </div>
</template>
