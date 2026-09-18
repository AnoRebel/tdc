<script setup lang="ts">
import { Fingerprint, Plus, Trash2 } from "lucide-vue-next";
import { Button } from "#layers/ui/app/components/ui/button";
import { Input } from "#layers/ui/app/components/ui/input";
import { Field } from "#layers/ui/app/components/ui/field";
import { Spinner } from "#layers/ui/app/components/ui/spinner";
import ConfirmDialog from "#layers/ui/app/components/ConfirmDialog.vue";

import { isApiError } from "@tdc/types";
import type { Passkey } from "@tdc/types";
import { whenBriefly } from "#layers/ui/app/lib/dates";

const { $t, $ts } = useI18n();

/**
 * Somebody's own passkeys: what they have, and adding or removing one.
 *
 * Draws nothing where the server cannot offer them, so the settings page can
 * include it without deciding.
 */

const { available, enrol, list, revoke } = usePasskeys();

const passkeys = ref<Passkey[]>([]);
const loading = ref(false);
const adding = ref(false);
const error = ref<string | null>(null);
const newLabel = ref("");
const removing = ref<Passkey | null>(null);

async function load() {
  if (!available.value) return;

  loading.value = true;
  try {
    passkeys.value = await list();
  } catch (cause) {
    error.value = isApiError(cause) ? cause.message : "Could not read your passkeys.";
  } finally {
    loading.value = false;
  }
}

/*
 * A name suggested from the device, so the field is not empty.
 *
 * Whatever somebody types wins; this is only so that pressing Add without
 * thinking about it still produces a name they will recognise later, rather
 * than a blank row among several blank rows.
 */
function suggestedName(): string {
  if (import.meta.server) return "This device";

  const agent = navigator.userAgent;
  if (/iPhone|iPad/.test(agent)) return "iPhone";
  if (/Android/.test(agent)) return "Android phone";
  if (/Mac/.test(agent)) return "Mac";
  if (/Windows/.test(agent)) return "Windows PC";
  if (/Linux/.test(agent)) return "Linux PC";
  return "This device";
}

async function add() {
  error.value = null;
  adding.value = true;

  try {
    const created = await enrol(newLabel.value.trim() || suggestedName());
    passkeys.value = [...passkeys.value, created];
    newLabel.value = "";
  } catch (cause) {
    // Dismissing the prompt is a decision, not a failure.
    if (
      cause instanceof DOMException &&
      (cause.name === "NotAllowedError" || cause.name === "AbortError")
    ) {
      return;
    }
    error.value = isApiError(cause) ? cause.message : "That passkey could not be added.";
  } finally {
    adding.value = false;
  }
}

async function confirmRemoval() {
  const passkey = removing.value;
  removing.value = null;
  if (!passkey) return;

  error.value = null;
  try {
    await revoke(passkey.id);
    passkeys.value = passkeys.value.filter((held) => held.id !== passkey.id);
  } catch (cause) {
    // The server explains the one refusal that is not about an attacker: this
    // being the only way left in.
    error.value = isApiError(cause) ? cause.message : "That passkey could not be removed.";
  }
}

onMounted(load);
</script>

<template>
  <section v-if="available" class="space-y-4">
    <div>
      <h2 class="text-sm font-semibold">{{ $t("auth.passkeys") }}</h2>
      <p class="mt-1 text-sm text-muted-foreground">
        {{ $t("auth.passkeysHint") }}
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
      {{ $t("auth.readingPasskeys") }}
    </p>

    <!-- Nothing yet, said as an invitation rather than as an absence. -->
    <p v-else-if="passkeys.length === 0" class="text-sm text-muted-foreground">
      {{ $t("auth.noPasskeysYet") }}
    </p>

    <ul v-else class="divide-y rounded-lg border">
      <li v-for="passkey in passkeys" :key="passkey.id" class="flex items-center gap-3 px-3 py-2.5">
        <Fingerprint class="size-4 shrink-0 text-muted-foreground" aria-hidden="true" />

        <div class="min-w-0 flex-1">
          <p class="truncate text-sm font-medium">{{ passkey.label || $t("auth.unnamed") }}</p>
          <p class="text-xs text-muted-foreground">
            {{ $t("auth.addedWhen", { when: whenBriefly(passkey.added) })
            }}<template v-if="passkey.lastUsed">
              · {{ $t("auth.lastUsedWhen", { when: whenBriefly(passkey.lastUsed) }) }}</template
            >
            <!-- Never used is worth saying: it is how somebody spots one they
                 enrolled and forgot, or one they did not enrol at all. -->
            <template v-else> · {{ $t("auth.neverUsed") }}</template>
          </p>
        </div>

        <Button
          variant="ghost"
          class="size-8 shrink-0 p-0 text-muted-foreground hover:text-destructive"
          :label="$ts('auth.removeNamed', { name: passkey.label || $ts('auth.thisPasskey') })"
          @click="removing = passkey"
        >
          <Trash2 class="size-4" aria-hidden="true" />
        </Button>
      </li>
    </ul>

    <form class="flex items-end gap-2" @submit.prevent="add">
      <div class="flex-1">
        <Field :label="$ts('auth.nameForNewPasskey')">
          <template #default="{ id, invalid, describedBy }">
            <Input
              :id="id"
              v-model="newLabel"
              :placeholder="suggestedName()"
              :invalid="invalid"
              :described-by="describedBy"
            />
          </template>
        </Field>
      </div>

      <Button type="submit" class="h-9 gap-2" :disabled="adding" :label="$ts('auth.addPasskey')">
        <Spinner v-if="adding" size="sm" />
        <Plus v-else class="size-4" aria-hidden="true" />
        {{ adding ? $t("auth.waitingForDevice") : $t("auth.add") }}
      </Button>
    </form>

    <ConfirmDialog
      v-if="removing"
      :title="$ts('auth.removeThisPasskey')"
      :detail="removing.label || $ts('auth.unnamed')"
      :confirm-label="$ts('action.remove')"
      @confirm="confirmRemoval"
      @cancel="removing = null"
    />
  </section>
</template>
