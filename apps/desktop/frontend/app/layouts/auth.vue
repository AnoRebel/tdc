<script setup lang="ts">
/**
 * The desktop sign-in layout: one centred column, not the web's two panels.
 *
 * The web splits the page — identity on one half, the form on the other —
 * because a browser tab is wide and a lone card in the middle of it is the
 * default every site arrives with. A desktop window is smaller and already
 * framed by the operating system's own chrome, with the application's name in
 * the title bar and its icon in the dock; the room a second panel would fill is
 * room this window does not have. So the form stands on its own, centred, with
 * the product named once above it.
 */
</script>

<template>
  <div class="grid min-h-dvh place-items-center bg-background px-6 py-10">
    <div class="w-full max-w-sm">
      <div class="mb-8 text-center">
        <div
          class="mx-auto mb-3 grid size-12 place-items-center rounded-xl bg-primary text-lg font-semibold text-primary-foreground"
        >
          T
        </div>
        <h1 class="text-lg font-semibold">TDC</h1>
      </div>

      <slot />
    </div>
  </div>
</template>
