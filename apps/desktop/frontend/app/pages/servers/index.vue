<script setup lang="ts">
import { Button } from "#layers/ui/app/components/ui/button";
import { Input } from "#layers/ui/app/components/ui/input";
import * as Api from "#bindings/api";

const { $t, $ts, $localeRoute: localeRoute } = useI18n();

/**
 * Choosing which server to talk to.
 *
 * The first thing somebody meets, and the one screen the web client has no use
 * for: a browser is already at a server, and this application is installed once
 * and pointed wherever its owner belongs.
 */
useHead({ title: $ts("nav.servers") });

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

/*
 * The server somebody is removing, held until they confirm.
 *
 * Removing takes the stored session with it, so the next launch needs a
 * password — and the button sits beside "Switch to" in a list, where a
 * misplaced press is easy.
 */
const removing = ref<KnownServer | null>(null);

async function confirmRemoval() {
  const server = removing.value;
  if (!server) return;

  removing.value = null;
  await forget(server.url);
}

async function addServer() {
  refusal.value = "";

  const failure = await add(typed.value);
  if (failure) {
    refusal.value = failure.message;
    return;
  }

  typed.value = "";

  // A server was chosen, so the next thing is signing in to it. The reachable
  // check inside add() has already passed, so this does not send anybody to a
  // form that cannot work.
  await navigateTo(localeRoute({ name: "signin" }));
}

/** Switches to a server already added, then goes on to sign in to it. */
async function useAndSignIn(url: string) {
  await use(url);
  await navigateTo(localeRoute({ name: "signin" }));
}
</script>

<template>
  <main class="mx-auto grid min-h-dvh max-w-lg place-items-center p-8">
    <div class="w-full">
      <h1 class="text-lg font-semibold">{{ $t("desktop.servers.yours") }}</h1>
      <p class="mt-1 text-sm text-muted-foreground">
        {{ $t("desktop.servers.hint") }}
      </p>

      <form class="mt-6 flex gap-2" @submit.prevent="addServer">
        <label class="sr-only" for="address">{{ $t("desktop.servers.address") }}</label>
        <Input
          id="address"
          v-model="typed"
          class="flex-1"
          placeholder="tdc.or.tz"
          autocomplete="off"
          :label="$ts('desktop.servers.address')"
          :invalid="Boolean(refusal)"
          :described-by="refusal ? 'refusal' : undefined"
          :disabled="loading"
        />
        <Button type="submit" :disabled="loading || !typed.trim()">
          {{ loading ? $t("desktop.checking") : $t("auth.add") }}
        </Button>
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
        <h2 class="text-xs font-semibold">{{ $t("desktop.servers.signInEachTime") }}</h2>
        <p class="mt-1 text-xs whitespace-pre-line text-muted-foreground">{{ storageAdvice }}</p>
      </section>

      <ul v-if="servers.length" class="mt-6 space-y-1">
        <li
          v-for="server in servers"
          :key="server.url"
          class="flex items-center gap-2 rounded-lg border border-border bg-surface px-3 py-2"
        >
          <span class="min-w-0 flex-1 truncate text-sm">{{ server.url }}</span>

          <NuxtLink
            v-if="server.current"
            :to="localeRoute({ name: 'signin' })"
            class="btn btn-primary shrink-0 text-xs"
          >
            {{ $t("auth.signIn") }}
          </NuxtLink>
          <Button
            v-else
            variant="outline"
            size="small"
            class="shrink-0"
            @click="useAndSignIn(server.url)"
          >
            {{ $t("auth.signIn") }}
          </Button>

          <!-- Removing a server takes its session with it, which is said here
               rather than only in the code: somebody expecting to stay signed
               in would be surprised, and on a shared machine that matters. -->
          <Button
            variant="danger"
            size="small"
            class="shrink-0"
            :label="$ts('desktop.servers.removeNamed', { url: server.url })"
            @click="removing = server"
          >
            {{ $t("action.remove") }}
          </Button>
        </li>
      </ul>

      <p v-else class="mt-6 text-xs text-muted-foreground">{{ $t("desktop.servers.none") }}</p>
    </div>

    <ConfirmDialog
      v-if="removing"
      :title="$ts('desktop.servers.removeTitle')"
      :detail="$ts('desktop.servers.removeDetail', { url: removing.url })"
      :confirm-label="$ts('action.remove')"
      @cancel="removing = null"
      @confirm="confirmRemoval"
    />
  </main>
</template>
