<script setup lang="ts">
import { isApiError } from "@tdc/types";
import { Spinner } from "#layers/ui/app/components/ui/spinner";

definePageMeta({ layout: "auth" });

useHead({ title: "Create an account" });

const { register } = useAuth();
const router = useRouter();
const { $localeRoute: localeRoute } = useI18n();

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
    await router.push(localeRoute({ name: "index" }));
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
  <div>
    <h1 class="text-xl font-semibold">Create an account</h1>
    <p class="mt-1 text-sm text-muted-foreground">
      Your username becomes your address on this server.
    </p>

    <form class="mt-6 space-y-4" @submit.prevent="submit">
      <p
        v-if="error"
        class="rounded-md bg-destructive/10 px-3 py-2 text-sm text-destructive"
        role="alert"
      >
        {{ error }}
      </p>

      <div>
        <label for="username" class="mb-1.5 block text-sm font-medium">Username</label>
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
        <label for="displayName" class="mb-1.5 block text-sm font-medium">
          Display name
          <span class="font-normal text-muted-foreground">(optional)</span>
        </label>
        <input
          id="displayName"
          v-model="displayName"
          class="input"
          name="displayName"
          autocomplete="name"
        />
      </div>

      <div>
        <label for="email" class="mb-1.5 block text-sm font-medium">
          Email
          <span class="font-normal text-muted-foreground">(optional)</span>
        </label>
        <input
          id="email"
          v-model="email"
          class="input"
          name="email"
          type="email"
          autocomplete="email"
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
          autocomplete="new-password"
          required
        />
      </div>

      <button type="submit" class="btn btn-primary h-9 w-full" :disabled="submitting">
        <Spinner v-if="submitting" size="sm" />
        {{ submitting ? "Creating account" : "Create account" }}
      </button>
    </form>

    <p class="mt-6 text-center text-sm text-muted-foreground">
      Already have an account?
      <NuxtLink
        :to="localeRoute({ name: 'signin' })"
        class="font-medium text-primary hover:underline"
      >
        Sign in
      </NuxtLink>
    </p>
  </div>
</template>
