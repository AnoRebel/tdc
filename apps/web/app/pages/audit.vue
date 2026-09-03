<script setup lang="ts">
import { isApiError } from "@tdc/types";

/**
 * The audit log.
 *
 * Reaching this page requires the audit-read permission, which the server
 * checks on the request. The permission check below only decides what to show;
 * a visitor who navigates here without it sees the refusal the server gave.
 */

definePageMeta({ middleware: "auth" });

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

function when(value: string): string {
  return new Date(value).toLocaleString();
}
</script>

<template>
  <div class="page">
    <header class="bar">
      <h1>Audit log</h1>
      <NuxtLink class="link-button" to="/">Back</NuxtLink>
    </header>

    <p v-if="error" class="error" role="alert">
      {{ error }}
      <span v-if="requiredPermission">
        You would need the <code>{{ requiredPermission }}</code> permission.
      </span>
    </p>

    <table v-else class="entries">
      <caption class="lead">
        Every security- and administration-relevant action, most recent first. Reading this log is
        itself recorded.
      </caption>
      <thead>
        <tr>
          <th scope="col">When</th>
          <th scope="col">Who</th>
          <th scope="col">Action</th>
          <th scope="col">Target</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="entry in entries" :key="entry.id">
          <td>{{ when(entry.occurredAt) }}</td>
          <td>{{ entry.actor }}</td>
          <td>
            <code>{{ entry.action }}</code>
          </td>
          <td class="target">
            <span v-if="entry.targetKind">{{ entry.targetKind }}</span>
            <span v-else class="none">—</span>
          </td>
        </tr>
      </tbody>
    </table>
  </div>
</template>

<style scoped>
.page {
  max-width: 60rem;
  margin: 0 auto;
  padding: 1rem;
}

.bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 1rem;
  padding: 1rem 0;
  border-bottom: 1px solid var(--border);
}

h1 {
  margin: 0;
  font-size: 1.5rem;
}

.link-button {
  padding: 0.5rem 0.9rem;
  border: 1px solid var(--border);
  border-radius: var(--radius);
  color: var(--text);
  text-decoration: none;
  font-weight: 600;
}

.entries {
  width: 100%;
  margin-top: 1.5rem;
  border-collapse: collapse;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius);
}

.lead {
  padding: 0.75rem;
  color: var(--text-muted);
  font-size: 0.9375rem;
  text-align: left;
  caption-side: top;
}

th,
td {
  padding: 0.6rem 0.75rem;
  text-align: left;
  border-bottom: 1px solid var(--border);
  font-size: 0.9375rem;
}

th {
  background: var(--surface-sunken);
  font-size: 0.8125rem;
}

tbody tr:last-child td {
  border-bottom: none;
}

code {
  font-size: 0.8125rem;
}

.none {
  color: var(--text-muted);
}
</style>
