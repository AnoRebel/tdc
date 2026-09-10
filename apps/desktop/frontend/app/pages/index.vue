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
  /*
   * Nowhere to go yet sends somebody to choose a server.
   *
   * The first thing this application needs is an address: it is installed once
   * and pointed at whichever server its owner belongs to, and every screen
   * beyond this one needs to know which.
   */
  const current = await Api.Service.CurrentServer();
  if (!current) {
    await navigateTo("/servers");
    return;
  }

  bridge.value = "ready";
  detail.value = current;
});

/*
 * The menu, answered.
 *
 * There is no search or channel list yet, so those report that they arrived —
 * which is what makes the menu demonstrably wired rather than merely present.
 * Each is replaced by the real screen as that screen lands.
 */
const lastCommand = ref("");
function acknowledge(what: string) {
  lastCommand.value = what;
}

/*
 * Appearance, which is the one setting this client can already offer.
 *
 * There is no settings screen here yet, and the theme is a choice somebody
 * makes on first opening an application rather than later — so the toggle is
 * on this screen, and the Settings menu item opens it rather than reporting
 * that it was pressed.
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
      <p class="mt-2 text-sm text-muted-foreground">The desktop client. Signing in comes next.</p>

      <p
        data-testid="bridge"
        :data-state="bridge"
        class="mt-6 text-xs"
        :class="bridge === 'broken' ? 'text-destructive' : 'text-muted-foreground'"
      >
        <template v-if="bridge === 'asking'">Looking for your server…</template>
        <template v-else-if="bridge === 'ready'">Connected to {{ detail }}.</template>
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
