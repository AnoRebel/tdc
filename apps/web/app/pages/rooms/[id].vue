<script setup lang="ts">
/**
 * One conversation, on the web.
 *
 * The conversation itself is the shared ChatRoom — the same component the
 * desktop client renders. This page is only what is particular to the web: the
 * route it reads the room from, the sign-in middleware, and the application
 * shell it sits in. Everything a person sees and does inside a conversation is
 * shared, so the two clients cannot drift.
 */
definePageMeta({ middleware: "auth", layout: "app" });

const route = useRoute();
const roomId = computed(() => String(route.params.id));
</script>

<template>
  <ChatRoom :room-id="roomId" />
</template>
