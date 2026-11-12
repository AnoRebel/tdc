<script setup lang="ts">
import { isApiError } from "@tdc/types";
import { ArrowLeft, Download, ShieldAlert } from "lucide-vue-next";
import { fully } from "#layers/ui/app/lib/dates";

const { $t, $ts, $localeRoute: localeRoute } = useI18n();

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
    header: $t("admin.when"),
    cell: ({ row }: { row: { original: AuditEntry } }) => when(row.original.occurredAt),
  },
  { accessorKey: "actor", header: $t("admin.who") },
  { accessorKey: "action", header: $t("admin.action") },
  {
    accessorKey: "targetKind",
    header: $t("admin.target"),
    // An entry about the server itself has no target, and an empty cell reads
    // as a missing value rather than an absent one.
    cell: ({ row }: { row: { original: AuditEntry } }) => row.original.targetKind ?? "—",
  },
];

useHead({ title: $ts("moderation.auditLog") });

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
const download = useDownload();

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
    error.value = $ts("admin.couldNotLoadAuditLog");
  }
}

/*
 * The whole log, saved as a file.
 *
 * Through the client's own download rather than a link built here: the web
 * client still hands the browser a plain link, which streams straight to disk
 * — the thing the newline-delimited format exists for — while the desktop
 * client, whose session the webview cannot carry, fetches through Go instead.
 */
const exportFailed = ref(false);

async function exportLog() {
  exportFailed.value = false;
  try {
    await download("/api/admin/audit/export", "audit-log.ndjson");
  } catch {
    exportFailed.value = true;
  }
}

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
        :aria-label="$ts('admin.backToSettings')"
      >
        <ArrowLeft class="size-4" />
      </NuxtLink>
      <h1 class="text-sm font-semibold">{{ $t("moderation.auditLog") }}</h1>
      <span v-if="!error" class="tabular ml-auto text-xs text-muted-foreground">
        {{ $t("admin.mostRecentCount", { count: entries.length }) }}
      </span>

      <!-- The whole log, not the page above it. Kept beside the listing
           because an operator who wants a copy is usually looking at it. -->
      <button v-if="!error" type="button" class="btn btn-outline" @click="exportLog">
        <Download class="size-3.5" />
        {{ $t("admin.export") }}
      </button>
    </header>

    <p
      v-if="exportFailed"
      class="mx-4 mt-4 max-w-4xl rounded-md bg-destructive/10 px-3 py-2 text-sm text-destructive"
      role="alert"
    >
      {{ $t("error.couldNotReach") }}
    </p>

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
            {{ $t("admin.needsPermissionBefore") }}
            <code class="rounded bg-muted px-1 py-0.5 font-mono text-xs">
              {{ requiredPermission }}
            </code>
            {{ $t("admin.needsPermissionAfter") }}
          </p>
        </div>
      </div>

      <template v-else>
        <p class="text-sm text-muted-foreground">
          {{ $t("admin.auditLogHint") }}
        </p>

        <UiDataTable
          class="mt-4 bg-surface"
          :columns="columns"
          :data="entries"
          :caption="$ts('admin.recordedActionsMostRecentFirst')"
          :empty="$ts('admin.nothingRecordedYet')"
        />
      </template>
    </div>
  </div>
</template>
