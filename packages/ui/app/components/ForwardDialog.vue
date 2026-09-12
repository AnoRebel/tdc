<script setup lang="ts">
import type { Room } from "@tdc/types";
import { Forward, Hash, Lock, Search } from "lucide-vue-next";
import { Button } from "#layers/ui/app/components/ui/button";

/**
 * Choosing where to forward.
 *
 * Only conversations this person is in, because forwarding into a channel you
 * have not joined would be posting in it — and the server refuses that, so
 * offering it here would only produce an error the reader cannot act on.
 */

const props = defineProps<{
  count: number;
  rooms: Room[];
}>();

const emit = defineEmits<{ cancel: []; forward: [roomId: string] }>();

const filter = ref("");
const field = ref<HTMLInputElement>();

onMounted(() => field.value?.focus());

const matching = computed(() => {
  const term = filter.value.trim().toLowerCase();
  if (!term) return props.rooms;

  return props.rooms.filter(
    (room) =>
      room.name.toLowerCase().includes(term) || (room.slug ?? "").toLowerCase().includes(term),
  );
});

function iconFor(room: Room) {
  return room.visibility === "private" ? Lock : Hash;
}
</script>

<template>
  <div
    class="fixed inset-0 z-50 flex items-start justify-center bg-background/70 p-4 backdrop-blur-sm"
    role="dialog"
    aria-modal="true"
    aria-label="Forward"
    @click.self="emit('cancel')"
    @keydown.esc="emit('cancel')"
  >
    <div
      class="mt-[12vh] w-[min(28rem,100%)] overflow-hidden rounded-xl border border-border bg-card shadow-2xl"
    >
      <header class="border-b border-border px-4 py-3">
        <h2 class="flex items-center gap-2 text-sm font-semibold">
          <Forward class="size-4" />
          {{ count === 1 ? "Forward this message" : `Forward ${count} messages` }}
        </h2>
      </header>

      <div class="border-b border-border px-3">
        <div class="relative">
          <Search
            class="pointer-events-none absolute top-1/2 left-2 size-3.5 -translate-y-1/2 text-muted-foreground"
          />
          <input
            ref="field"
            v-model="filter"
            type="text"
            placeholder="Search conversations"
            class="h-11 w-full bg-transparent pl-7 text-sm outline-none placeholder:text-muted-foreground"
            aria-label="Search conversations"
          />
        </div>
      </div>

      <div class="max-h-72 overflow-y-auto p-1.5">
        <p v-if="!rooms.length" class="px-2 py-8 text-center text-xs text-muted-foreground">
          There is nowhere else to forward this. Join another conversation first.
        </p>
        <p v-else-if="!matching.length" class="px-2 py-8 text-center text-xs text-muted-foreground">
          Nothing matches &ldquo;{{ filter.trim() }}&rdquo;.
        </p>

        <button
          v-for="room in matching"
          :key="room.id"
          type="button"
          class="flex w-full cursor-pointer items-center gap-2 rounded-lg px-2 py-2 text-left text-sm transition-colors hover:bg-accent"
          @click="emit('forward', room.id)"
        >
          <component :is="iconFor(room)" class="size-4 shrink-0 text-muted-foreground" />
          <span class="truncate font-medium">{{ room.name }}</span>
          <span v-if="room.topic" class="truncate text-xs text-muted-foreground">
            {{ room.topic }}
          </span>
        </button>
      </div>

      <footer class="flex justify-end border-t border-border px-4 py-3">
        <Button variant="ghost" @click="emit('cancel')">Cancel</Button>
      </footer>
    </div>
  </div>
</template>
