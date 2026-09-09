<script setup lang="ts">
/*
 * What opens when the application starts.
 *
 * A placeholder until server selection exists — 9.11 decides which server this
 * client talks to, and until it does there is nothing to sign in to.
 */
useHead({ title: "TDC" });
</script>

<template>
  <main class="grid min-h-dvh place-items-center bg-background p-8 text-foreground">
    <div class="max-w-sm text-center">
      <h1 class="text-lg font-semibold">TDC</h1>
      <p class="mt-2 text-sm text-muted-foreground">
        The desktop client. Choosing a server comes next.
      </p>
    </div>
  </main>
</template>
