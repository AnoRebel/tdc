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
import * as Api from "#bindings/api";

useHead({ title: "TDC" });

const bridge = ref<"asking" | "ready" | "broken">("asking");
const detail = ref("");

onMounted(async () => {
  try {
    /*
     * Deliberately a request with nowhere to go: no server has been chosen,
     * so Go answers with ErrNoServer. That refusal is the proof — it could
     * only have come from the Go side, so the whole path worked.
     */
    await Api.Service.Send({ method: "GET", path: "/api/health" });
    // Which should not happen. If it did, something is answering that should
    // not be, and saying so is better than a green light that means nothing.
    bridge.value = "broken";
    detail.value = "A request succeeded before a server was chosen.";
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    if (message.includes("no server chosen")) {
      bridge.value = "ready";
    } else {
      bridge.value = "broken";
      detail.value = message;
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
