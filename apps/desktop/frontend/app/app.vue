<script setup lang="ts">
import { Button } from "#layers/ui/app/components/ui/button";
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
    title="Quit TDC?"
    detail="Anything you have written and not sent will be lost."
    confirm-label="Quit"
    @cancel="confirmingQuit = false"
    @confirm="quit"
  />

  <!-- What this is, which is the first thing anybody reporting a problem is
       asked for. -->
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
          <dt class="w-20 shrink-0 text-muted-foreground">Version</dt>
          <dd class="tabular">{{ about.version }}</dd>
        </div>
        <div class="flex gap-2">
          <dt class="w-20 shrink-0 text-muted-foreground">Platform</dt>
          <dd class="tabular">{{ about.platform }}</dd>
        </div>
        <div class="flex gap-2">
          <dt class="w-20 shrink-0 text-muted-foreground">Licence</dt>
          <dd>{{ about.licence }}</dd>
        </div>
      </dl>

      <p class="mt-4 text-xs text-muted-foreground">Free and open source. Self-hosted, always.</p>

      <div class="mt-4 flex justify-end">
        <Button @click="showingAbout = false">Close</Button>
      </div>
    </div>
  </div>

  <ConfirmDialog
    v-if="askingCloseBehaviour"
    title="Keep TDC running when you close it?"
    detail="It will stay in the tray so new messages still reach you. You can change this later in the tray menu, and quitting from there always quits."
    confirm-label="Keep it running"
    cancel-label="Quit on close"
    @cancel="chooseCloseBehaviour('quit')"
    @confirm="chooseCloseBehaviour('tray')"
  />
</template>
