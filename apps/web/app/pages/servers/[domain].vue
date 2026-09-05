<script setup lang="ts">
import type { ConnectedServer, Room } from "@tdc/types";
import { isApiError } from "@tdc/types";
import { Globe, Hash, Lock, Server, WifiOff } from "lucide-vue-next";
import { Skeleton } from "~/components/ui/skeleton";

/**
 * A connected server, and the rooms it publishes.
 *
 * What comes back has already been filtered by the peer's own visibility and
 * by each room's, so everything received here can be shown: the client is not
 * the thing deciding what is hidden.
 */

definePageMeta({ middleware: "auth", layout: "app" });

useHead({
  // The server being viewed, so several open at once stay distinguishable.
  title: computed(() => String(useRoute().params.domain ?? "Server")),
});

const route = useRoute();
const api = useApi();

const domain = computed(() => String(route.params.domain));

const server = ref<ConnectedServer | null>(null);
const rooms = ref<Room[]>([]);
const error = ref<string | null>(null);
const loading = ref(true);

async function load() {
  loading.value = true;
  error.value = null;
  try {
    server.value = await api.get<ConnectedServer>(
      `/api/federation/servers/${encodeURIComponent(domain.value)}`,
    );
    rooms.value = await api.get<Room[]>(
      `/api/federation/servers/${encodeURIComponent(domain.value)}/rooms`,
    );
  } catch (cause) {
    error.value = isApiError(cause) ? cause.message : "Could not reach that server.";
  } finally {
    loading.value = false;
  }
}

watch(domain, load, { immediate: true });

/** The icon that carries a server's visibility, matching the sidebar. */
const visibilityIcon = computed(() =>
  server.value?.visibility === "public"
    ? Globe
    : server.value?.visibility === "limited"
      ? Server
      : Lock,
);

const visibilityLabel = computed(() =>
  server.value?.visibility === "public"
    ? "Open to the network"
    : server.value?.visibility === "limited"
      ? "Open to listed servers"
      : "Private",
);
</script>

<template>
  <div class="flex-1 overflow-y-auto">
    <header class="flex h-12 shrink-0 items-center gap-3 border-b px-4">
      <component :is="visibilityIcon" class="size-4 shrink-0 text-muted-foreground" />
      <div class="min-w-0">
        <h1 class="truncate text-sm leading-tight font-semibold">
          {{ server?.name ?? domain }}
        </h1>
        <p class="truncate text-xs leading-tight text-muted-foreground">{{ domain }}</p>
      </div>
      <span v-if="server" class="ml-auto shrink-0 text-xs text-muted-foreground">
        {{ visibilityLabel }}
      </span>
    </header>

    <div class="max-w-3xl px-4 py-6">
      <p
        v-if="error"
        class="rounded-md bg-destructive/10 px-3 py-2 text-sm text-destructive"
        role="alert"
      >
        {{ error }}
      </p>

      <div v-else-if="loading" class="space-y-2">
        <Skeleton class="h-4 w-2/3" />
        <Skeleton v-for="n in 3" :key="n" class="h-10 rounded-lg" />
      </div>

      <template v-else-if="server">
        <p v-if="server.description" class="max-w-[65ch] text-sm text-muted-foreground">
          {{ server.description }}
        </p>

        <div
          v-if="!server.reachable"
          class="mt-4 flex items-center gap-2.5 rounded-lg border border-warning/30 bg-warning/8 px-3 py-2.5 text-sm"
        >
          <WifiOff class="size-4 shrink-0 text-warning" />
          <span>
            This server is not reachable right now. Its rooms will appear again when it returns.
          </span>
        </div>

        <section class="mt-6">
          <h2 class="text-sm font-semibold">Rooms</h2>

          <ul v-if="rooms.length" class="mt-3 space-y-1">
            <li v-for="room in rooms" :key="room.id">
              <NuxtLink
                :to="`/rooms/${room.id}`"
                class="flex items-center gap-2 rounded-lg border bg-surface px-3 py-2.5 text-sm transition hover:border-border-strong hover:bg-accent"
              >
                <component
                  :is="room.visibility === 'private' ? Lock : Hash"
                  class="size-3.5 shrink-0 text-muted-foreground"
                />
                <span class="truncate font-medium">{{ room.name }}</span>
                <span v-if="room.topic" class="truncate text-xs text-muted-foreground">
                  {{ room.topic }}
                </span>
              </NuxtLink>
            </li>
          </ul>

          <p v-else class="mt-3 text-sm text-muted-foreground">
            This server publishes no rooms you can browse.
          </p>
        </section>
      </template>
    </div>
  </div>
</template>
