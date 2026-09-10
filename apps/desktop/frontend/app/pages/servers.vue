<script setup lang="ts">
import * as Api from "#bindings/api";

/**
 * Choosing which server to talk to.
 *
 * The first thing somebody meets, and the one screen the web client has no use
 * for: a browser is already at a server, and this application is installed once
 * and pointed wherever its owner belongs.
 */
useHead({ title: "Servers" });

const { servers, loading, refresh, add, use, forget } = useServers();

const typed = ref("");
const refusal = ref("");

/*
 * Where this machine can keep a session, which somebody needs to know before
 * signing in rather than after wondering why they are asked every time.
 */
const storage = ref("");
const storageAdvice = ref("");

onMounted(async () => {
  await refresh();

  storage.value = await Api.Service.CredentialStorage();
  if (storage.value === "none") {
    storageAdvice.value = await Api.Service.CredentialAdvice();
  }
});

async function addServer() {
  refusal.value = "";

  const failure = await add(typed.value);
  if (failure) {
    refusal.value = failure.message;
    return;
  }

  typed.value = "";
}
</script>

<template>
  <main class="mx-auto grid min-h-dvh max-w-lg place-items-center p-8">
    <div class="w-full">
      <h1 class="text-lg font-semibold">Your servers</h1>
      <p class="mt-1 text-sm text-muted-foreground">
        TDC is self-hosted, so there is no single place to sign in to — add the address of the
        server you belong to.
      </p>

      <form class="mt-6 flex gap-2" @submit.prevent="addServer">
        <label class="sr-only" for="address">Server address</label>
        <input
          id="address"
          v-model="typed"
          type="text"
          class="input flex-1"
          placeholder="tdc.or.tz"
          autocomplete="off"
          :aria-invalid="Boolean(refusal)"
          :aria-describedby="refusal ? 'refusal' : undefined"
          :disabled="loading"
        />
        <button type="submit" class="btn btn-primary" :disabled="loading || !typed.trim()">
          {{ loading ? "Checking…" : "Add" }}
        </button>
      </form>

      <!-- The server's own words. It refused for a reason it knows and this
           does not, and the reason is usually actionable: a certificate, a
           name that does not resolve, nothing listening. -->
      <p v-if="refusal" id="refusal" role="alert" class="mt-2 text-xs text-destructive">
        {{ refusal }}
      </p>

      <!-- Said before signing in, not after. Somebody who is not going to be
           remembered should know that while choosing, and be told what would
           change it — a machine that cannot keep a session is a working
           application, not a broken one. -->
      <section
        v-if="storageAdvice"
        class="mt-6 rounded-lg border border-warning/40 bg-warning/10 p-3"
      >
        <h2 class="text-xs font-semibold">You will have to sign in each time</h2>
        <p class="mt-1 text-xs whitespace-pre-line text-muted-foreground">{{ storageAdvice }}</p>
      </section>

      <ul v-if="servers.length" class="mt-6 space-y-1">
        <li
          v-for="server in servers"
          :key="server.url"
          class="flex items-center gap-2 rounded-lg border border-border bg-surface px-3 py-2"
        >
          <span class="min-w-0 flex-1 truncate text-sm">{{ server.url }}</span>

          <span
            v-if="server.current"
            class="shrink-0 rounded-full bg-primary px-2 py-0.5 text-[0.6875rem] font-semibold text-primary-foreground"
          >
            Current
          </span>
          <button
            v-else
            type="button"
            class="btn btn-outline shrink-0 text-xs"
            @click="use(server.url)"
          >
            Switch to
          </button>

          <!-- Removing a server takes its session with it, which is said here
               rather than only in the code: somebody expecting to stay signed
               in would be surprised, and on a shared machine that matters. -->
          <button
            type="button"
            class="btn btn-ghost shrink-0 text-xs text-destructive"
            :aria-label="`Remove ${server.url} and sign out of it`"
            @click="forget(server.url)"
          >
            Remove
          </button>
        </li>
      </ul>

      <p v-else class="mt-6 text-xs text-muted-foreground">No servers yet.</p>
    </div>
  </main>
</template>
