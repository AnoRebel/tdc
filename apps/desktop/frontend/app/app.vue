<script setup lang="ts">
import { Button } from "#layers/ui/app/components/ui/button";

const { $t, $ts } = useI18n();
/*
 * The desktop client.
 *
 * Deliberately thin: every screen comes from the shared layer, and this file
 * exists to mount them. Anything that appears here rather than there is
 * something the web client would not have, which is a decision worth making
 * on purpose rather than by drift.
 */
useHead({
  titleTemplate: (page?: string) => (page ? `${page} — TDC` : "TDC"),
});

/*
 * The colours, resolved the same way the web client resolves them: this
 * person's choice over the operator's default over the palette's own. Both
 * clients call the one function, so a change to the order cannot apply to one
 * of them and not the other.
 */
useAppearanceSync();

/*
 * Pressing a system notification opens what it was about.
 *
 * Here rather than on a screen, because one may be pressed at any time —
 * including while looking at something else entirely.
 */
useNotificationOpens();

/*
 * The state those commands act on, shared with the application shell.
 *
 * The shell is a layout below this component, so nothing here can reach into
 * it — these are the same pieces of Nuxt state the layout reads, which is how a
 * menu item at the root opens a palette rendered several levels down.
 */
const searching = useState("search.open", () => false);
const creatingChannel = useState("channel.creating", () => false);
const showingShortcuts = useState("shortcuts.open", () => false);

/*
 * The View menu, answered.
 *
 * Here rather than on a screen, for the reason the About and quit handlers are:
 * a menu item can be chosen from any of them, and a listener living on one page
 * hears nothing from the rest.
 *
 * Search is the one that matters most, and the one that looked broken. The menu
 * carries Ctrl+K as an accelerator, and GTK claims an accelerator before the
 * webview is offered the keystroke — so the shared layer's own binding never
 * fires on this client, however well it works in a browser. The menu is
 * therefore the only thing that can open the palette from that key, and this is
 * what connects it.
 */

useMenuCommands({
  search: () => {
    searching.value = true;
  },
  newChannel: () => {
    creatingChannel.value = true;
  },
  keyboardHelp: () => {
    showingShortcuts.value = true;
  },
});

/*
 * The page's ground, painted at the same alpha as the window.
 *
 * Here rather than on a screen: every screen sits on this ground, and one that
 * matched only on the appearance page would be solid everywhere else.
 */
const { matchTheWindow } = useWindowOpacity();
onMounted(() => void matchTheWindow());

/*
 * What closing the window does, asked the first time it happens.
 *
 * Here rather than on a screen: a window can be closed from any of them, and a
 * listener that lived on one page would hear nothing from the others.
 */
const { asking: askingCloseBehaviour, choose: chooseCloseBehaviour } = useCloseBehaviour();

/*
 * About and quitting, both reachable from the menu and the tray.
 *
 * Here for the same reason as the close behaviour: either can be asked for
 * from any screen, so a listener on one page would hear nothing from the rest.
 */
const { about, showingAbout, confirmingQuit, quit } = useAppCommands();
</script>

<template>
  <NuxtLayout>
    <NuxtPage />
  </NuxtLayout>

  <!--
    Guessing what closing means loses one of two things somebody cares about:
    the window they meant to tuck away, or the application they meant to quit.
    Asking once is cheaper than being wrong.
  -->
  <!--
    Quitting asks, because only this side knows whether anything is unsent.
    Go sends the request and waits; nothing ends until this answers.
  -->
  <ConfirmDialog
    v-if="confirmingQuit"
    :title="$ts('desktop.quitTitle')"
    :detail="$ts('desktop.quitDetail')"
    :confirm-label="$ts('desktop.quit')"
    @cancel="confirmingQuit = false"
    @confirm="quit"
  />

  <!-- What this is, which is the first thing anybody reporting a problem is
       asked for. -->
  <!-- A modal backdrop: role="dialog" with aria-modal, dismissed by clicking
       outside it or pressing Escape. A dialog is not a button; the role is
       what names it to assistive technology, and the Escape handler beside
       the click is the keyboard path the rule exists to require. -->
  <!-- eslint-disable-next-line vuejs-accessibility/no-static-element-interactions -->
  <div
    v-if="showingAbout && about"
    class="fixed inset-0 z-50 grid place-items-center bg-black/50 p-4"
    role="dialog"
    aria-modal="true"
    aria-labelledby="about-title"
    @click.self="showingAbout = false"
    @keydown.esc="showingAbout = false"
  >
    <div class="w-full max-w-sm rounded-xl border border-border bg-surface-raised p-5 shadow-xl">
      <h2 id="about-title" class="text-base font-semibold">{{ about.name }}</h2>
      <p class="mt-1 text-sm text-muted-foreground">{{ about.description }}</p>

      <dl class="mt-4 space-y-1 text-xs">
        <div class="flex gap-2">
          <dt class="w-20 shrink-0 text-muted-foreground">{{ $t("desktop.about.version") }}</dt>
          <dd class="tabular">{{ about.version }}</dd>
        </div>
        <div class="flex gap-2">
          <dt class="w-20 shrink-0 text-muted-foreground">{{ $t("desktop.about.platform") }}</dt>
          <dd class="tabular">{{ about.platform }}</dd>
        </div>
        <div class="flex gap-2">
          <dt class="w-20 shrink-0 text-muted-foreground">{{ $t("desktop.about.licence") }}</dt>
          <dd>{{ about.licence }}</dd>
        </div>
      </dl>

      <p class="mt-4 text-xs text-muted-foreground">{{ $t("auth.freeAndOpenSource") }}</p>

      <div class="mt-4 flex justify-end">
        <Button @click="showingAbout = false">{{ $t("action.close") }}</Button>
      </div>
    </div>
  </div>

  <ConfirmDialog
    v-if="askingCloseBehaviour"
    :title="$ts('desktop.keepRunningTitle')"
    :detail="$ts('desktop.keepRunningDetail')"
    :confirm-label="$ts('desktop.keepRunning')"
    :cancel-label="$ts('desktop.quitOnClose')"
    @cancel="chooseCloseBehaviour('quit')"
    @confirm="chooseCloseBehaviour('tray')"
  />
</template>
