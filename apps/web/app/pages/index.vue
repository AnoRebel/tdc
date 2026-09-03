<script setup lang="ts">
import type { Session } from "@tdc/types";
import { isApiError } from "@tdc/types";

definePageMeta({ middleware: "auth" });

const { user, permissions, can, signOut, listSessions, revokeSession } = useAuth();
const router = useRouter();

const sessions = ref<Session[]>([]);
const error = ref<string | null>(null);
const busy = ref<string | null>(null);

async function loadSessions() {
  try {
    sessions.value = await listSessions();
  } catch (cause) {
    error.value = isApiError(cause) ? cause.message : "Could not load your sessions.";
  }
}

await loadSessions();

async function endSession(id: string) {
  error.value = null;
  busy.value = id;
  try {
    await revokeSession(id);
    await loadSessions();
  } catch (cause) {
    error.value = isApiError(cause) ? cause.message : "Could not end that session.";
  } finally {
    busy.value = null;
  }
}

async function leave() {
  await signOut();
  await router.push("/signin");
}

/** Formats a timestamp for display in the reader's own locale. */
function when(value: string): string {
  return new Date(value).toLocaleString();
}
</script>

<template>
  <div class="page">
    <header class="bar">
      <div>
        <strong>{{ user?.displayName }}</strong>
        <span class="address">{{ user?.address }}</span>
      </div>
      <button class="secondary" type="button" @click="leave">Sign out</button>
    </header>

    <main class="content">
      <p v-if="error" class="error" role="alert">{{ error }}</p>

      <section class="panel">
        <h2>Your account</h2>
        <dl>
          <dt>Username</dt>
          <dd>{{ user?.username }}</dd>
          <dt>Display name</dt>
          <dd>{{ user?.displayName }}</dd>
          <dt>Address</dt>
          <dd>{{ user?.address }}</dd>
        </dl>
      </section>

      <section class="panel">
        <h2>Signed-in devices</h2>
        <p class="lead">
          Ending a session signs that device out immediately. Your current session is marked.
        </p>

        <ul class="sessions">
          <li v-for="session in sessions" :key="session.id">
            <div>
              <span class="agent">{{ session.userAgent || "Unknown client" }}</span>
              <span v-if="session.current" class="badge">This device</span>
              <span class="detail">
                {{ session.ipAddress ? `${session.ipAddress} · ` : "" }}last active
                {{ when(session.lastSeenAt) }}
              </span>
            </div>
            <button
              v-if="!session.current"
              class="danger"
              type="button"
              :disabled="busy === session.id"
              @click="endSession(session.id)"
            >
              {{ busy === session.id ? "Ending…" : "End session" }}
            </button>
          </li>
        </ul>
      </section>

      <section class="panel">
        <h2>What you can do here</h2>
        <p class="lead">
          These are the permissions your roles grant. The server checks every action against them
          independently.
        </p>
        <ul class="permissions">
          <li v-for="permission in permissions" :key="permission">
            <code>{{ permission }}</code>
          </li>
        </ul>
        <p v-if="permissions.length === 0" class="lead">
          You hold no special permissions on this server.
        </p>
      </section>

      <section v-if="can('server.audit.read')" class="panel">
        <h2>Administration</h2>
        <p class="lead">You hold administrative permissions on this server.</p>
        <NuxtLink class="link-button" to="/audit">View the audit log</NuxtLink>
      </section>
    </main>
  </div>
</template>

<style scoped>
.page {
  max-width: 48rem;
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
  flex-wrap: wrap;
}

.bar div {
  display: grid;
}

.address {
  color: var(--text-muted);
  font-size: 0.875rem;
}

.content {
  display: grid;
  gap: 1.5rem;
  padding: 1.5rem 0;
}

.panel {
  padding: 1.25rem;
  border: 1px solid var(--border);
  border-radius: var(--radius);
  background: var(--surface);
}

.panel h2 {
  margin: 0 0 0.5rem;
  font-size: 1.125rem;
}

.lead {
  margin: 0 0 1rem;
  color: var(--text-muted);
  font-size: 0.9375rem;
}

dl {
  display: grid;
  grid-template-columns: auto 1fr;
  gap: 0.35rem 1rem;
  margin: 0;
}

dt {
  font-weight: 600;
}

dd {
  margin: 0;
}

.sessions {
  display: grid;
  gap: 0.75rem;
  margin: 0;
  padding: 0;
  list-style: none;
}

.sessions li {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 1rem;
  padding: 0.75rem;
  border: 1px solid var(--border);
  border-radius: var(--radius);
  flex-wrap: wrap;
}

.agent {
  display: block;
  font-weight: 600;
  font-size: 0.9375rem;
}

.detail {
  display: block;
  color: var(--text-muted);
  font-size: 0.8125rem;
}

.badge {
  display: inline-block;
  margin-left: 0.5rem;
  padding: 0.1rem 0.45rem;
  border-radius: 999px;
  background: color-mix(in srgb, var(--success) 18%, transparent);
  color: var(--success);
  font-size: 0.75rem;
  font-weight: 600;
}

.permissions {
  display: flex;
  flex-wrap: wrap;
  gap: 0.4rem;
  margin: 0;
  padding: 0;
  list-style: none;
}

.permissions code {
  padding: 0.15rem 0.45rem;
  border-radius: var(--radius);
  background: var(--surface-sunken);
  font-size: 0.8125rem;
}

.link-button {
  display: inline-block;
  padding: 0.6rem 1rem;
  border: 1px solid var(--border);
  border-radius: var(--radius);
  color: var(--text);
  text-decoration: none;
  font-weight: 600;
}
</style>
