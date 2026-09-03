<script setup lang="ts">
import { isApiError } from "@tdc/types";

definePageMeta({ layout: false });

const { signIn, setupStatus } = useAuth();
const router = useRouter();

const username = ref("");
const password = ref("");
const error = ref<string | null>(null);
const submitting = ref(false);

// A server with no accounts cannot be signed into, so send the first visitor
// to setup rather than showing them a form that can only fail.
const status = await setupStatus();
if (status.setupRequired) {
  await router.replace("/setup");
}

async function submit() {
  error.value = null;
  submitting.value = true;

  try {
    await signIn({ username: username.value, password: password.value });
    // Return the person to whatever they were trying to reach.
    const next = router.currentRoute.value.query.next;
    await router.push(typeof next === "string" ? next : "/");
  } catch (cause) {
    if (isApiError(cause) && cause.code === "rate-limited") {
      const wait = cause.retryAfterSeconds;
      error.value = wait
        ? `Too many attempts. Try again in ${Math.ceil(wait / 60)} minute(s).`
        : "Too many attempts. Try again later.";
    } else if (isApiError(cause)) {
      error.value = cause.message;
    } else {
      error.value = "Could not reach the server.";
    }
  } finally {
    submitting.value = false;
  }
}
</script>

<template>
  <main class="centred">
    <form class="card" @submit.prevent="submit">
      <h1>Sign in</h1>

      <p v-if="error" class="error" role="alert">{{ error }}</p>

      <div class="field">
        <label for="username">Username or email</label>
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
        <label for="password">Password</label>
        <input
          id="password"
          v-model="password"
          name="password"
          type="password"
          autocomplete="current-password"
          required
        />
      </div>

      <button type="submit" :disabled="submitting">
        {{ submitting ? "Signing in…" : "Sign in" }}
      </button>

      <p class="alternative">
        No account yet?
        <NuxtLink to="/register">Create one</NuxtLink>
      </p>
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
  max-width: 24rem;
  padding: 2rem;
  border: 1px solid var(--border);
  border-radius: var(--radius);
  background: var(--surface);
}

h1 {
  margin: 0 0 1.5rem;
  font-size: 1.5rem;
}

button {
  width: 100%;
}

.alternative {
  margin: 1.25rem 0 0;
  font-size: 0.9375rem;
  color: var(--text-muted);
  text-align: center;
}
</style>
