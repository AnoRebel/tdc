<script setup lang="ts">
import { Copy } from "lucide-vue-next";

import type { Invocation } from "@tdc/types";
import { isApiError } from "@tdc/types";
import { Button } from "#layers/ui/app/components/ui/button";
import { Input } from "#layers/ui/app/components/ui/input";
import { howLongAgo } from "#layers/ui/app/lib/dates";

/**
 * Where an application is told about presses on its messages, when it holds no
 * live connection to hear them on.
 *
 * Setting an address answers with the secret every invocation is signed with,
 * shown here once — exactly as a webhook's is. Clearing it stops invocations,
 * and anything still waiting is cancelled rather than sent.
 */

const props = defineProps<{ applicationId: string; current?: string }>();
const emit = defineEmits<{ saved: [] }>();

const { $t, $ts } = useI18n();
const integrations = useIntegrations();
const address = ref(props.current ?? "");
const secret = ref<string | null>(null);
const error = ref<string | null>(null);
const recent = ref<Invocation[]>([]);
const { copy, copied } = useClipboard({ legacy: true });

/** Each state in words, so the list never leans on a colour. */
function stateOf(state: Invocation["state"]): string {
  switch (state) {
    case "delivered":
      return $ts("integrations.invocationDelivered");
    case "abandoned":
      return $ts("integrations.invocationAbandoned");
    case "cancelled":
      return $ts("integrations.invocationCancelled");
    default:
      return $ts("integrations.invocationWaiting");
  }
}

async function load() {
  try {
    recent.value = (await integrations.invocations(props.applicationId)).slice(0, 5);
  } catch {
    recent.value = [];
  }
}

onMounted(() => void load());

async function save() {
  error.value = null;
  try {
    const answer = await integrations.setInvocation(props.applicationId, address.value.trim());
    secret.value = answer.secret ?? null;
    emit("saved");
    await load();
  } catch (cause) {
    error.value = isApiError(cause) ? cause.message : $ts("integrations.couldNotSave");
  }
}
</script>

<template>
  <div>
    <h3 class="text-xs font-medium">{{ $t("integrations.invocationAddress") }}</h3>
    <p class="mt-0.5 text-[0.6875rem] text-muted-foreground">
      {{ $t("integrations.invocationHint") }}
    </p>
    <form class="mt-2 flex gap-2" @submit.prevent="save">
      <Input
        v-model="address"
        type="url"
        class="min-w-0 flex-1"
        :aria-label="$ts('integrations.invocationAddress')"
        placeholder="https://bot.example.org/presses"
      />
      <Button type="submit" variant="outline" size="small">
        {{ $t("integrations.saveInvocation") }}
      </Button>
    </form>
    <p v-if="error" class="mt-1 text-xs text-destructive" role="alert">{{ error }}</p>

    <div
      v-if="secret"
      class="mt-2 rounded-md border border-primary/40 bg-primary/5 p-2"
      role="status"
    >
      <p class="text-xs font-medium">{{ $t("integrations.invocationSecretOnce") }}</p>
      <div class="mt-1 flex items-center gap-2">
        <code
          class="min-w-0 flex-1 truncate rounded bg-muted px-2 py-1 font-mono text-xs select-all"
          >{{ secret }}</code
        >
        <Button variant="outline" size="small" @click="copy(secret)">
          <Copy class="size-3.5" aria-hidden="true" />
          {{ copied ? $t("integrations.copied") : $t("integrations.copy") }}
        </Button>
        <Button variant="ghost" size="small" @click="secret = null">
          {{ $t("integrations.done") }}
        </Button>
      </div>
    </div>

    <ul
      v-if="recent.length"
      class="mt-2 space-y-0.5"
      :aria-label="$ts('integrations.recentInvocations')"
    >
      <li
        v-for="invocation in recent"
        :key="invocation.id"
        class="text-[0.6875rem]"
        data-invocation
      >
        {{ stateOf(invocation.state) }}
        · {{ $t("integrations.attemptsCount", { count: invocation.attempts }) }} ·
        {{ howLongAgo(invocation.created) }}
        <template v-if="invocation.lastStatus"> · HTTP {{ invocation.lastStatus }}</template>
      </li>
    </ul>
  </div>
</template>
