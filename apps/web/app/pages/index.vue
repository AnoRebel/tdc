<script setup lang="ts">
import type { Room } from "@tdc/types";
import { Hash, MessagesSquare } from "lucide-vue-next";

/**
 * The view before a conversation is chosen.
 *
 * An empty state rather than a list: the sidebar already holds every room, so
 * repeating them here would be the same information twice. What is useful is
 * saying what to do next.
 */

definePageMeta({ middleware: "auth", layout: "app" });

useHead({ title: "Home" });

const roomsApi = useRooms();
const { user } = useAuth();

const rooms = ref<Room[]>([]);
try {
  rooms.value = await roomsApi.list();
} catch {
  // The sidebar reports its own failures; this view degrades to the prompt
  // below rather than an error of its own.
}

const joined = computed(() => rooms.value.filter((room) => room.member));

/** Just the first name, which is how people greet each other. */
const firstName = computed(() => user.value?.displayName?.split(" ")[0] ?? "");
</script>

<template>
  <div class="grid flex-1 place-items-center p-8">
    <div class="w-full max-w-sm text-center">
      <span class="mx-auto grid size-12 place-items-center rounded-xl bg-muted">
        <MessagesSquare class="size-5 text-muted-foreground" />
      </span>

      <h1 class="mt-4 text-lg font-semibold">Karibu, {{ firstName }}</h1>

      <p class="mt-1.5 text-sm text-muted-foreground text-pretty">
        {{
          joined.length
            ? "Pick a conversation from the sidebar to carry on where you left off."
            : "Join a channel from the sidebar, or create one to get started."
        }}
      </p>

      <div v-if="joined.length" class="mt-6 space-y-1 text-left">
        <p
          class="px-1 pb-1 text-[0.6875rem] font-semibold tracking-wide text-muted-foreground uppercase"
        >
          Jump back in
        </p>
        <NuxtLink
          v-for="room in joined.slice(0, 4)"
          :key="room.id"
          :to="`/rooms/${room.id}`"
          class="flex items-center gap-2 rounded-lg border bg-surface px-3 py-2 text-sm transition hover:border-border-strong hover:bg-accent"
        >
          <Hash class="size-3.5 shrink-0 text-muted-foreground" />
          <span class="truncate font-medium">{{ room.name }}</span>
          <span v-if="room.topic" class="truncate text-xs text-muted-foreground">
            {{ room.topic }}
          </span>
        </NuxtLink>
      </div>
    </div>
  </div>
</template>
