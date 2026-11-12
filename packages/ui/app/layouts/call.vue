<script setup lang="ts">
/**
 * A page about one call — a call link — and nothing else around it.
 *
 * Open to somebody with no account, which the application's own layout is not:
 * no sidebar of conversations to load, nothing that needs membership. Once
 * somebody is signed in — as a member or as a guest of the call — the call
 * host is here, so the call itself can run.
 */

const { signedIn } = useAuth();
const { offers } = useServerInfo();
const hosting = computed(() => signedIn.value && offers("calls"));
</script>

<template>
  <div class="flex min-h-dvh flex-col bg-background text-foreground">
    <main class="flex flex-1 flex-col">
      <slot />
    </main>
    <LazyCallHost v-if="hosting" />
  </div>
</template>
