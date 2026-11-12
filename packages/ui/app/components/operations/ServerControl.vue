<script setup lang="ts">
import { isApiError } from "@tdc/types";
import { CircleCheck, CircleX, RefreshCw, Power } from "lucide-vue-next";
import type {
  NodesOverview,
  Operation,
  ServerNode,
} from "#layers/ui/app/composables/useOperations";
import { Button } from "#layers/ui/app/components/ui/button";
import { onDayAtTime } from "#layers/ui/app/lib/dates";

/**
 * Reloading and restarting the server's nodes.
 *
 * Reload re-reads the configuration without dropping anybody and says what it
 * could not apply. Restart checks the next process would start before it stops
 * this one, says who is connected and which calls are running before anybody
 * confirms, and then waits for the node to come back and says whether it did.
 */
const emit = defineEmits<{ restarted: [] }>();
const { $t, $ts } = useI18n();
const operations = useOperations();

const overview = ref<NodesOverview | null>(null);
const history = ref<Operation[]>([]);
const message = ref<{ ok: boolean; text: string } | null>(null);
const working = ref<string | null>(null);
const confirming = ref<ServerNode | null>(null);

async function load() {
  try {
    const [nodes, ops] = await Promise.all([operations.nodes(), operations.list()]);
    overview.value = nodes;
    history.value = ops.filter((op) => op.kind === "reload" || op.kind === "restart").slice(0, 10);
  } catch {
    overview.value = null;
  }
}
onMounted(load);

async function reload(node: ServerNode) {
  working.value = node.id;
  message.value = null;
  try {
    let op = await operations.reload(node.id);
    if (op.status === "pending") op = (await operations.follow(op.id, () => {}, 60_000)) ?? op;
    const result = op.result as { applied?: string[]; needsRestart?: string[] };
    message.value = {
      ok: op.status === "succeeded",
      text:
        op.status === "succeeded"
          ? $ts("operations.server.reloaded", {
              applied: (result.applied ?? []).join(", ") || "—",
              restart: (result.needsRestart ?? []).join(", ") || "—",
            })
          : (op.error ?? $ts("operations.server.reloadFailed")),
    };
  } catch (cause) {
    message.value = {
      ok: false,
      text: isApiError(cause) ? cause.message : $ts("operations.server.reloadFailed"),
    };
  } finally {
    working.value = null;
    await load();
  }
}

/**
 * Restarts a node and waits for it.
 *
 * The page's own requests fail while the node is away; the operation is read
 * from whichever node answers, so with several nodes this does not depend on
 * reaching the one that restarted.
 */
async function restart(node: ServerNode) {
  confirming.value = null;
  working.value = node.id;
  message.value = { ok: true, text: $ts("operations.server.restarting") };
  try {
    const op = await operations.restart(node.id);
    const ended = await operations.follow(op.id, () => {}, 90_000);
    if (!ended) {
      message.value = { ok: false, text: $ts("operations.server.didNotReturn") };
    } else if (ended.status === "succeeded") {
      emit("restarted");
      const result = ended.result as { version?: string; startedAt?: string };
      message.value = {
        ok: true,
        text: $ts("operations.server.restarted", {
          version: result.version ?? "",
          started: result.startedAt ? onDayAtTime(result.startedAt) : "",
        }),
      };
    } else {
      message.value = { ok: false, text: ended.error ?? $ts("operations.server.restartFailed") };
    }
  } catch (cause) {
    message.value = {
      ok: false,
      text: isApiError(cause) ? cause.message : $ts("operations.server.restartFailed"),
    };
  } finally {
    working.value = null;
    await load();
  }
}

const plan = computed(() => overview.value?.restart);
const howItRestarts = computed(() => {
  if (!plan.value) return "";
  if (plan.value.refusal) return plan.value.refusal;
  const how =
    plan.value.method === "supervisor"
      ? $ts("operations.server.bySupervisor", { supervisor: plan.value.supervisor ?? "" })
      : $ts("operations.server.inPlace");
  return `${how} ${plan.value.recovers ? $ts("operations.server.recovers") : $ts("operations.server.mayNotRecover")}`;
});

const impact = computed(() => {
  const node = confirming.value;
  if (!node || !overview.value) return "";
  return $ts("operations.server.confirmDetail", {
    people: node.connections,
    calls: overview.value.calls,
  });
});
</script>

<template>
  <section v-if="overview">
    <h2 class="text-sm font-semibold">{{ $t("operations.server.title") }}</h2>
    <p class="mt-0.5 text-xs text-muted-foreground">{{ $t("operations.server.hint") }}</p>
    <p class="mt-1 text-xs" :class="plan?.refusal ? 'text-warning' : 'text-muted-foreground'">
      {{ howItRestarts }}
    </p>

    <ul class="mt-3 space-y-2">
      <li
        v-for="node in overview.nodes"
        :key="node.id"
        class="flex flex-wrap items-center justify-between gap-2 rounded-lg border bg-surface px-3 py-2"
      >
        <div class="min-w-0 text-sm">
          <p class="font-medium">
            {{ node.host }}
            <span
              v-if="node.this"
              class="ml-1 rounded bg-primary/15 px-1.5 py-0.5 text-[0.6875rem] text-primary"
            >
              {{ $t("operations.server.thisNode") }}
            </span>
            <span v-if="!node.alive" class="ml-1 text-xs text-muted-foreground">{{
              $t("operations.server.gone")
            }}</span>
          </p>
          <p class="text-xs text-muted-foreground">
            {{
              $t("operations.server.nodeDetail", {
                version: node.version,
                started: onDayAtTime(node.startedAt),
                people: node.connections,
              })
            }}
          </p>
        </div>
        <div class="flex gap-2">
          <Button
            variant="outline"
            size="small"
            :disabled="!node.alive || working !== null"
            @click="reload(node)"
          >
            <RefreshCw class="size-3.5" />
            {{ $t("operations.server.reload") }}
          </Button>
          <Button
            variant="danger"
            size="small"
            :disabled="!node.alive || working !== null || Boolean(node.this && plan?.refusal)"
            @click="confirming = node"
          >
            <Power class="size-3.5" />
            {{ $t("operations.server.restart") }}
          </Button>
        </div>
      </li>
    </ul>

    <p
      v-if="message"
      role="status"
      class="mt-3 flex items-start gap-1.5 rounded-md px-3 py-2 text-sm"
      :class="message.ok ? 'bg-primary/8' : 'bg-destructive/10 text-destructive'"
    >
      <CircleCheck v-if="message.ok" class="mt-0.5 size-4 shrink-0" />
      <CircleX v-else class="mt-0.5 size-4 shrink-0" />
      {{ message.text }}
    </p>

    <details v-if="history.length" class="mt-3 text-xs">
      <summary class="cursor-pointer text-muted-foreground">
        {{ $t("operations.server.recent") }}
      </summary>
      <ul class="mt-1 space-y-1">
        <li v-for="op in history" :key="op.id">
          {{ onDayAtTime(op.requestedAt) }} · {{ $t(`operations.kind.${op.kind}`) }} ·
          {{ $t(`operations.status.${op.status}`) }} · {{ op.requestedBy }}
          <span v-if="op.error" class="text-destructive">— {{ op.error }}</span>
        </li>
      </ul>
    </details>
  </section>

  <ConfirmDialog
    v-if="confirming"
    :title="$ts('operations.server.confirmTitle', { host: confirming.host })"
    :detail="impact"
    :confirm-label="$ts('operations.server.restart')"
    :cancel-label="$ts('operations.cancel')"
    @cancel="confirming = null"
    @confirm="restart(confirming)"
  />
</template>
