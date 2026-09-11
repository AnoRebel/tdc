<script setup lang="ts">
/**
 * One conversation, on the desktop.
 *
 * The conversation is the shared ChatRoom — the same component the web renders.
 * This page is only the desktop's own frame around it: the route it reads the
 * room from, the auth-and-server middleware, and the application shell. What a
 * person does inside a conversation is shared, so the two clients cannot drift.
 */
definePageMeta({ middleware: "auth", layout: "app" });

const route = useRoute();
const roomId = computed(() => String(route.params.id));
</script>

<template>
  <ChatRoom :room-id="roomId" />
</template>
