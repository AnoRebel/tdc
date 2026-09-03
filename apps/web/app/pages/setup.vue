<script setup lang="ts">
import { isApiError } from "@tdc/types";

/**
 * First-run setup: creates the server's first administrator.
 *
 * Reachable only while the server has no accounts. The server refuses once one
 * exists, so this page cannot be used later to mint an administrator; the
 * redirect below is a courtesy, not the control.
 */

const { setupStatus, completeSetup } = useAuth();
const router = useRouter();

const username = ref("");
const password = ref("");
const displayName = ref("");
const error = ref<string | null>(null);
const submitting = ref(false);

const status = await setupStatus();
if (!status.setupRequired) {
  await router.replace("/signin");
}

async function submit() {
  error.value = null;
  submitting.value = true;

  try {
    await completeSetup({
      username: username.value,
      password: password.value,
      displayName: displayName.value || undefined,
    });
    await router.push("/");
  } catch (cause) {
    error.value = isApiError(cause) ? cause.message : "Could not reach the server.";
  } finally {
    submitting.value = false;
  }
}
</script>

<template>
  <main class="centred">
    <form class="card" @submit.prevent="submit">
      <h1>Set up this server</h1>
      <p class="lead">
        This server has no accounts yet. The account you create now will administer it.
      </p>

      <p v-if="error" class="error" role="alert">{{ error }}</p>

      <div class="field">
        <label for="username">Username</label>
        <input
          id="username"
          v-model="username"
          name="username"
          autocomplete="username"
          required
          autofocus
        />
      </div>

      <div class="field">
        <label for="displayName">Display name <span class="optional">(optional)</span></label>
        <input id="displayName" v-model="displayName" name="displayName" autocomplete="name" />
      </div>

      <div class="field">
        <label for="password">Password</label>
        <input
          id="password"
          v-model="password"
          name="password"
          type="password"
          autocomplete="new-password"
          required
          minlength="12"
        />
        <span class="hint">At least 12 characters.</span>
      </div>

      <button type="submit" :disabled="submitting">
        {{ submitting ? "Setting up…" : "Create administrator" }}
      </button>
    </form>
  </main>
</template>

<style scoped>
.centred {
  display: grid;
  place-items: center;
  min-height: 100vh;
  padding: 1rem;
}

.card {
  width: 100%;
  max-width: 26rem;
  padding: 2rem;
  border: 1px solid var(--border);
  border-radius: var(--radius);
  background: var(--surface);
}

h1 {
  margin: 0 0 0.5rem;
  font-size: 1.5rem;
}

.lead {
  margin: 0 0 1.5rem;
  color: var(--text-muted);
  font-size: 0.9375rem;
}

.optional {
  font-weight: 400;
  color: var(--text-muted);
}

button {
  width: 100%;
}
</style>
