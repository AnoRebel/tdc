<script setup lang="ts">
import { isApiError } from "@tdc/types";
import { ArrowLeft, Download, ShieldAlert } from "lucide-vue-next";

/**
 * The audit log.
 *
 * Reaching this page requires the audit-read permission, which the server
 * checks on the request. The permission check below only decides what to show;
 * a visitor who navigates here without it sees the refusal the server gave.
 */

definePageMeta({ middleware: "auth", layout: "app" });

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
  return new Date(value).toLocaleString();
}
</script>

<template>
  <div class="flex-1 overflow-y-auto">
    <header class="flex h-12 shrink-0 items-center gap-3 border-b px-4">
      <NuxtLink to="/settings" class="btn btn-ghost size-7 px-0" aria-label="Back to settings">
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

        <!-- Scrolls inside its own container so the page never scrolls
             sideways on a narrow screen. -->
        <div class="mt-4 overflow-x-auto rounded-lg border bg-surface">
          <table class="w-full text-sm">
            <thead>
              <tr class="border-b text-left">
                <th scope="col" class="px-3 py-2 text-xs font-medium text-muted-foreground">
                  When
                </th>
                <th scope="col" class="px-3 py-2 text-xs font-medium text-muted-foreground">Who</th>
                <th scope="col" class="px-3 py-2 text-xs font-medium text-muted-foreground">
                  Action
                </th>
                <th scope="col" class="px-3 py-2 text-xs font-medium text-muted-foreground">
                  Target
                </th>
              </tr>
            </thead>
            <tbody>
              <tr
                v-for="entry in entries"
                :key="entry.id"
                class="border-b transition last:border-0 hover:bg-surface-sunken"
              >
                <td class="px-3 py-2 whitespace-nowrap">
                  <time class="text-muted-foreground">{{ when(entry.occurredAt) }}</time>
                </td>
                <td class="px-3 py-2">{{ entry.actor }}</td>
                <td class="px-3 py-2">
                  <code class="rounded bg-muted px-1.5 py-0.5 font-mono text-xs">
                    {{ entry.action }}
                  </code>
                </td>
                <td class="px-3 py-2 text-muted-foreground">
                  {{ entry.targetKind ?? "—" }}
                </td>
              </tr>
            </tbody>
          </table>

          <p
            v-if="entries.length === 0"
            class="px-3 py-8 text-center text-sm text-muted-foreground"
          >
            Nothing recorded yet.
          </p>
        </div>
      </template>
    </div>
  </div>
</template>
