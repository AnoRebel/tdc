<script setup lang="ts">
import { isApiError } from "@tdc/types";
import * as Api from "#bindings/api";

/**
 * Signing in to the chosen server.
 *
 * The form and what it does are the web client's, over the same shared useAuth
 * — the request goes through the desktop transport, and the session lands in
 * the Go client's jar rather than a browser's cookie store. What the desktop
 * adds is the offer to be remembered: the web leaves a session cookie the
 * browser keeps, and the desktop has no browser to keep one, so staying signed
 * in between launches means putting the credentials in the operating system's
 * keychain — which is a choice, made here, not a default.
 */

definePageMeta({ layout: "auth" });
useHead({ title: "Sign in" });

const { signIn } = useAuth();
const router = useRouter();

const server = ref("");
const username = ref("");
const password = ref("");
const remember = ref(false);
const error = ref<string | null>(null);
const submitting = ref(false);

// Whether this machine has anywhere safe to keep credentials. Without a
// keychain the offer would be a checkbox that silently does nothing, so it is
// not shown; the servers screen already explains why signing in each time is
// what happens here.
const keychain = ref(false);

// The username last remembered for this server, offered so somebody is not
// retyping what the machine already knows.
onMounted(async () => {
  try {
    const current = await Api.Service.CurrentServer();
    if (!current) {
      // No server chosen yet — that screen comes first.
      await router.replace("/servers");
      return;
    }
    server.value = current;

    try {
      keychain.value = (await Api.Service.CredentialStorage()) !== "none";
    } catch {
      keychain.value = false;
    }

    try {
      const remembered = await Api.Service.Recall(current);
      if (remembered) {
        username.value = remembered;
        // A remembered username means they asked to be remembered before, so
        // the box starts ticked — unticking it is how somebody stops being.
        remember.value = true;
      }
    } catch {
      // Nothing remembered, or no keychain. The empty form is correct.
    }
  } catch (cause) {
    error.value = "Startup error: " + String((cause as Error)?.message ?? cause);
  }
});

async function submit() {
  error.value = null;
  submitting.value = true;

  try {
    await signIn({ username: username.value, password: password.value });

    // Kept only if asked, and only where there is a keychain to keep it in. The
    // session token stays on the Go side; what is handed over is what the
    // person typed, so Go can sign in again with it after a relaunch. Declining
    // simply stores nothing — the server stays chosen and the session stays
    // live for this run; it is just not remembered past it. Forgetting the
    // server here would undo the sign-in that just succeeded.
    if (remember.value && keychain.value) {
      try {
        await Api.Service.Remember(server.value, username.value, password.value, "");
      } catch {
        // Being remembered failed, but signing in did not. Better to be signed
        // in and not remembered than to lose the sign-in over the keychain.
      }
    }

    await router.push("/");
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
    <div class="mb-6 text-center">
      <h2 class="text-base font-semibold">Sign in</h2>
      <p v-if="server" class="mt-1 truncate text-xs text-muted-foreground">{{ server }}</p>
    </div>

    <form class="space-y-4" @submit.prevent="submit">
      <p
        v-if="error"
        class="rounded-md bg-destructive/10 px-3 py-2 text-sm text-destructive"
        role="alert"
      >
        {{ error }}
      </p>

      <div>
        <label for="username" class="mb-1.5 block text-sm font-medium">Username or email</label>
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

      <label v-if="keychain" class="flex cursor-pointer items-center gap-2">
        <input v-model="remember" type="checkbox" class="accent-primary" />
        <span class="text-sm">Keep me signed in on this machine</span>
      </label>

      <button type="submit" class="btn btn-primary h-9 w-full" :disabled="submitting">
        {{ submitting ? "Signing in" : "Sign in" }}
      </button>
    </form>

    <p class="mt-6 text-center text-xs text-muted-foreground">
      Signing in to a different server?
      <NuxtLink to="/servers" class="font-medium text-primary hover:underline">
        Choose one
      </NuxtLink>
    </p>
  </div>
</template>
