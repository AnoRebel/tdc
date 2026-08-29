<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import type { ServerInfo } from "@tdc/types";

/**
 * The first screen: prove the client can reach a TDC server and report what it
 * found. Everything else in the application builds on this connection.
 */

const serverInfo = ref<ServerInfo | null>(null);
const error = ref<string | null>(null);
const loading = ref(true);

const apiBase = useRuntimeConfig().public.apiBase;

const status = computed(() => {
  if (loading.value) return "connecting";
  return error.value ? "unreachable" : "connected";
});

async function loadServerInfo() {
  loading.value = true;
  error.value = null;

  try {
    serverInfo.value = await $fetch<ServerInfo>(`${apiBase}/api/server`);
  } catch (cause) {
    // The message is for a developer running this locally; the production
    // client will present connection failures far more gently.
    error.value = cause instanceof Error ? cause.message : String(cause);
    serverInfo.value = null;
  } finally {
    loading.value = false;
  }
}

onMounted(loadServerInfo);
</script>

<template>
  <main class="page">
    <h1>TDC</h1>

    <p class="status" :data-status="status">
      {{ status }}
    </p>

    <dl v-if="serverInfo" class="details">
      <dt>Server</dt>
      <dd>{{ serverInfo.name }}</dd>

      <dt>Description</dt>
      <dd>{{ serverInfo.description }}</dd>

      <dt>Version</dt>
      <dd>{{ serverInfo.version }}</dd>

      <dt>Address</dt>
      <dd>{{ serverInfo.canonicalAddress || "not set" }}</dd>

      <dt>Language</dt>
      <dd>{{ serverInfo.defaultLanguage }}</dd>
    </dl>

    <p v-else-if="error" class="error">Could not reach a TDC server: {{ error }}</p>

    <button type="button" @click="loadServerInfo">Retry</button>
  </main>
</template>

<style>
:root {
  color-scheme: light dark;
  font-family: system-ui, sans-serif;
}

body {
  margin: 0;
}

.page {
  max-width: 40rem;
  margin: 0 auto;
  padding: 2rem 1rem;
}

.status {
  font-variant: small-caps;
  letter-spacing: 0.05em;
}

.status[data-status="connected"] {
  color: green;
}

.status[data-status="unreachable"] {
  color: crimson;
}

.details {
  display: grid;
  grid-template-columns: auto 1fr;
  gap: 0.25rem 1rem;
  margin: 1.5rem 0;
}

.details dt {
  font-weight: 600;
}

.details dd {
  margin: 0;
}

.error {
  color: crimson;
}
</style>
