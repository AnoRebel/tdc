<script setup lang="ts">
import { isApiError } from "@tdc/types";
import { ArrowLeft, Download, ShieldAlert } from "lucide-vue-next";
import { fully } from "#layers/ui/app/lib/dates";

const { $localeRoute: localeRoute } = useI18n();

/**
 * The audit log.
 *
 * Reaching this page requires the audit-read permission, which the server
 * checks on the request. The permission check below only decides what to show;
 * a visitor who navigates here without it sees the refusal the server gave.
 */

definePageMeta({ middleware: "auth", layout: "app" });

/*
 * The columns, described rather than laid out.
 *
 * Sorting and paging come from the description: the table knows a column of
 * timestamps can be ordered without this page saying how, and adding a column
 * is adding an entry here rather than four places in a template.
 */
const columns = [
  {
    accessorKey: "occurredAt",
    header: "When",
    cell: ({ row }: { row: { original: AuditEntry } }) => when(row.original.occurredAt),
  },
  { accessorKey: "actor", header: "Who" },
  { accessorKey: "action", header: "Action" },
  {
    accessorKey: "targetKind",
    header: "Target",
    // An entry about the server itself has no target, and an empty cell reads
    // as a missing value rather than an absent one.
    cell: ({ row }: { row: { original: AuditEntry } }) => row.original.targetKind ?? "—",
  },
];

useHead({ title: "Audit log" });

interface AuditEntry {
  id: number;
  occurredAt: string;
  actor: string;
  action: string;
  targetKind?: string;
  targetId?: string;
  sourceIp?: string;
}

const api = useApi();

const entries = ref<AuditEntry[]>([]);
const error = ref<string | null>(null);
const requiredPermission = ref<string | null>(null);

try {
  entries.value = await api.get<AuditEntry[]>("/api/admin/audit?limit=100");
} catch (cause) {
  if (isApiError(cause)) {
    error.value = cause.message;
    requiredPermission.value = cause.requiredPermission ?? null;
  } else {
    error.value = "Could not load the audit log.";
  }
}

/*
 * Where the export lives.
 *
 * A plain link rather than a fetch: the browser streams it straight to a file,
 * where fetching would hold the whole log in memory to hand it back — which is
 * the thing the newline-delimited format exists to avoid.
 */
const exportUrl = computed(() => `${useRuntimeConfig().public.apiBase}/api/admin/audit/export`);

function when(value: string): string {
  return fully(value);
}
</script>

<template>
  <div class="flex-1 overflow-y-auto">
    <header class="flex h-12 shrink-0 items-center gap-3 border-b px-4">
      <NuxtLink
        :to="localeRoute({ name: 'settings' })"
        class="btn btn-ghost size-7 px-0"
        aria-label="Back to settings"
      >
        <ArrowLeft class="size-4" />
      </NuxtLink>
      <h1 class="text-sm font-semibold">Audit log</h1>
      <span v-if="!error" class="tabular ml-auto text-xs text-muted-foreground">
        {{ entries.length }} most recent
      </span>

      <!-- The whole log, not the page above it. Kept beside the listing
           because an operator who wants a copy is usually looking at it. -->
      <a v-if="!error" :href="exportUrl" download="audit-log.ndjson" class="btn btn-outline">
        <Download class="size-3.5" />
        Export
      </a>
    </header>

    <div class="max-w-4xl px-4 py-6">
      <div
        v-if="error"
        class="flex gap-3 rounded-lg border border-destructive/30 bg-destructive/8 p-4"
        role="alert"
      >
        <ShieldAlert class="mt-0.5 size-4 shrink-0 text-destructive" />
        <div class="text-sm">
          <p class="font-medium text-destructive">{{ error }}</p>
          <p v-if="requiredPermission" class="mt-1 text-muted-foreground">
            This needs the
            <code class="rounded bg-muted px-1 py-0.5 font-mono text-xs">
              {{ requiredPermission }}
            </code>
            permission.
          </p>
        </div>
      </div>

      <template v-else>
        <p class="text-sm text-muted-foreground">
          Every security- and administration-relevant action, most recent first. Reading this log is
          itself recorded.
        </p>

        <UiDataTable
          class="mt-4 bg-surface"
          :columns="columns"
          :data="entries"
          caption="Recorded actions, most recent first"
          empty="Nothing recorded yet."
        />
      </template>
    </div>
  </div>
</template>
