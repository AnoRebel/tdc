<script setup lang="ts">
import { isApiError } from "@tdc/types";
import { ShieldCheck } from "lucide-vue-next";
import { Spinner } from "~/components/ui/spinner";

definePageMeta({ layout: "auth" });

useHead({ title: "Set up this server" });

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
  <div>
    <span class="grid size-9 place-items-center rounded-lg bg-primary/12">
      <ShieldCheck class="size-4 text-primary" />
    </span>

    <h1 class="mt-4 text-xl font-semibold">Set up this server</h1>
    <p class="mt-1 text-sm text-muted-foreground">
      This account becomes the server's administrator. It can only be created once.
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
        {{ submitting ? "Creating administrator" : "Create administrator" }}
      </button>
    </form>
  </div>
</template>
