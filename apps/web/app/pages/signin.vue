<script setup lang="ts">
import { isApiError } from "@tdc/types";
import { Spinner } from "~/components/ui/spinner";

definePageMeta({ layout: "auth" });

useHead({ title: "Sign in" });

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
  <div>
    <h1 class="text-xl font-semibold">Sign in</h1>
    <p class="mt-1 text-sm text-muted-foreground">Welcome back.</p>

    <form class="mt-6 space-y-4" @submit.prevent="submit">
      <p
        v-if="error"
        class="rounded-md bg-destructive/10 px-3 py-2 text-sm text-destructive"
        role="alert"
      >
        {{ error }}
      </p>

      <div>
        <label for="username" class="mb-1.5 block text-sm font-medium"> Username or email </label>
        <input
          id="username"
          v-model="username"
          class="input"
          name="username"
          autocomplete="username"
          required
          autofocus
        />
      </div>

      <div>
        <label for="password" class="mb-1.5 block text-sm font-medium">Password</label>
        <input
          id="password"
          v-model="password"
          class="input"
          name="password"
          type="password"
          autocomplete="current-password"
          required
        />
      </div>

      <button type="submit" class="btn btn-primary h-9 w-full" :disabled="submitting">
        <Spinner v-if="submitting" size="sm" />
        {{ submitting ? "Signing in" : "Sign in" }}
      </button>
    </form>

    <p class="mt-6 text-center text-sm text-muted-foreground">
      No account yet?
      <NuxtLink to="/register" class="font-medium text-primary hover:underline">
        Create one
      </NuxtLink>
    </p>
  </div>
</template>
