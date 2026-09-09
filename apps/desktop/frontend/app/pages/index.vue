<script setup lang="ts">
/*
 * What opens when the application starts.
 *
 * A placeholder until server selection exists — 9.11 decides which server this
 * client talks to, and until it does there is nothing to sign in to.
 *
 * It does two real things, neither of them decoration. It calls into Go, which
 * is the only evidence that the generated bindings, the alias and the runtime
 * line up — a build that compiles proves the types agree, not that the call
 * arrives. And it answers the application menu, so the menu items are
 * demonstrably connected rather than merely present.
 */
import { Unreachable } from "~/lib/transport";

useHead({ title: "TDC" });

const bridge = ref<"asking" | "ready" | "broken">("asking");
const detail = ref("");

const api = useApi();

onMounted(async () => {
  try {
    /*
     * Through the shared client, not the binding directly. That is the thing
     * worth checking: every screen above this calls useApi(), and this proves
     * the desktop adapter satisfies it end to end — the generated bindings,
     * the transport, and the shared client on top of them.
     *
     * Deliberately a request with nowhere to go: no server has been chosen, so
     * Go answers with ErrNoServer, which the transport reports as an
     * unreachable server rather than a refusal. That is the correct reading —
     * nothing answered — and it could only have come from the Go side.
     */
    await api.get("/api/health");
    // Which should not happen. If it did, something is answering that should
    // not be, and saying so is better than a green light that means nothing.
    bridge.value = "broken";
    detail.value = "A request succeeded before a server was chosen.";
  } catch (error) {
    if (error instanceof Unreachable && error.message.includes("no server chosen")) {
      bridge.value = "ready";
    } else {
      bridge.value = "broken";
      detail.value = error instanceof Error ? error.message : String(error);
    }
  }
});

/*
 * The menu, answered.
 *
 * There is no search or settings screen yet, so for now each one reports that
 * it arrived. That is not a placeholder for its own sake: it is what makes the
 * menu demonstrably wired, and each line is replaced by the real screen as
 * that screen lands rather than being removed.
 */
const lastCommand = ref("");
function acknowledge(what: string) {
  lastCommand.value = what;
}

/*
 * Appearance, which is the one setting this client can already offer.
 *
 * There is no settings screen here until server selection exists, and the
 * theme is a choice somebody makes on first opening an application rather
 * than later — so the toggle is on this screen, and the Settings menu item
 * opens it rather than reporting that it was pressed.
 */
const showAppearance = ref(false);

useMenuCommands({
  search: () => acknowledge("Search"),
  preferences: () => {
    showAppearance.value = true;
  },
  newChannel: () => acknowledge("New channel"),
  notifications: () => acknowledge("Notifications"),
  keyboardHelp: () => acknowledge("Keyboard shortcuts"),
});
</script>

<template>
  <main class="grid min-h-dvh place-items-center bg-background p-8 text-foreground">
    <div class="max-w-sm text-center">
      <h1 class="text-lg font-semibold">TDC</h1>
      <p class="mt-2 text-sm text-muted-foreground">
        The desktop client. Choosing a server comes next.
      </p>

      <p
        data-testid="bridge"
        :data-state="bridge"
        class="mt-6 text-xs"
        :class="bridge === 'broken' ? 'text-destructive' : 'text-muted-foreground'"
      >
        <template v-if="bridge === 'asking'">Checking the connection to the application…</template>
        <template v-else-if="bridge === 'ready'">Connected to the application.</template>
        <template v-else>Could not reach the application. {{ detail }}</template>
      </p>

      <p data-testid="menu-command" class="mt-2 text-xs text-muted-foreground">
        <template v-if="lastCommand">Menu: {{ lastCommand }}</template>
        <template v-else>Try the menu.</template>
      </p>

      <!-- Not in a sidebar, because there is not one yet. It moves into the
           footer with the rest of the shell once a server is chosen. -->
      <div class="mt-6 flex justify-center">
        <ThemeToggle v-model="showAppearance" />
      </div>
    </div>
  </main>
</template>
