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

/*
 * An issue's discussion is a room too, and a notification or a search result
 * about a comment arrives here. It belongs beside the issue it is about, so
 * that is where it goes — carrying the message to open at.
 */
const roomsApi = useRooms();
const router = useRouter();
const { $localeRoute: localeRoute } = useI18n();
watch(
  roomId,
  async (id) => {
    try {
      const room = await roomsApi.get(id);
      // An event's conversation belongs on the event's page, where its call
      // and its people are.
      if ((room.kind as string) === "event") {
        await router.replace(localeRoute({ name: "events-id", params: { id: room.id } }));
        return;
      }
      if ((room.kind as string) === "issue-discussion") {
        await router.replace(
          localeRoute({ name: "issues-ref", params: { ref: room.name }, query: route.query }),
        );
      }
    } catch {
      // ChatRoom reports what is wrong with the room; this only redirects.
    }
  },
  { immediate: true },
);
</script>

<template>
  <ChatRoom :room-id="roomId" />
</template>
