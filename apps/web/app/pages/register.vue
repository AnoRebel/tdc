<script setup lang="ts">
import { isApiError } from "@tdc/types";

const { register } = useAuth();
const router = useRouter();

const username = ref("");
const email = ref("");
const password = ref("");
const displayName = ref("");
const error = ref<string | null>(null);
const submitting = ref(false);

async function submit() {
  error.value = null;
  submitting.value = true;

  try {
    await register({
      username: username.value,
      password: password.value,
      email: email.value || undefined,
      displayName: displayName.value || undefined,
    });
    await router.push("/");
  } catch (cause) {
    // The server names which field is the problem — a taken username, a weak
    // password — so its message is shown rather than a generic one.
    error.value = isApiError(cause) ? cause.message : "Could not reach the server.";
  } finally {
    submitting.value = false;
  }
}
</script>

<template>
  <main class="centred">
    <form class="card" @submit.prevent="submit">
      <h1>Create an account</h1>

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
        <span class="hint">Letters, digits, underscore, dot and hyphen.</span>
      </div>

      <div class="field">
        <label for="displayName">Display name <span class="optional">(optional)</span></label>
        <input id="displayName" v-model="displayName" name="displayName" autocomplete="name" />
      </div>

      <div class="field">
        <label for="email">Email <span class="optional">(optional)</span></label>
        <input id="email" v-model="email" name="email" type="email" autocomplete="email" />
        <span class="hint">Used only to recover your account.</span>
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
        <span class="hint">
          At least 12 characters. A memorable phrase is stronger than a short, complicated word.
        </span>
      </div>

      <button type="submit" :disabled="submitting">
        {{ submitting ? "Creating…" : "Create account" }}
      </button>

      <p class="alternative">
        Already have an account?
        <NuxtLink to="/signin">Sign in</NuxtLink>
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
  max-width: 26rem;
  padding: 2rem;
  border: 1px solid var(--border);
  border-radius: var(--radius);
  background: var(--surface);
}

h1 {
  margin: 0 0 1.5rem;
  font-size: 1.5rem;
}

.optional {
  font-weight: 400;
  color: var(--text-muted);
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
