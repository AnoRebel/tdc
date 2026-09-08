<script setup lang="ts">
import type { ConnectedServer, Room, UnreadCounts } from "@tdc/types";
import type { Notification } from "@tdc/types";
import { Bell, Globe, Hash, Lock, Plus, Search, Server, Settings } from "lucide-vue-next";
import { Avatar } from "~/components/ui/avatar";
import { Kbd } from "~/components/ui/kbd";

/**
 * The persistent navigation.
 *
 * Rooms are always reachable from anywhere, which is what makes this an
 * application rather than a set of pages: moving between conversations should
 * not mean going back to a list first.
 */

const props = defineProps<{
  rooms: Room[];
  direct: Room[];
  unread: UnreadCounts;
  /** Rooms the person can see but has not joined. */
  available: Room[];
  /** Whether this person may create a channel at all. */
  canCreate: boolean;
  /**
   * Other servers this one is connected to. Empty when federation is
   * disabled, which is the default, so the section simply does not appear.
   */
  servers: ConnectedServer[];
}>();

const emit = defineEmits<{ create: []; search: [] }>();

const { user } = useAuth();
const route = useRoute();

const joined = computed(() => props.rooms);
const conversations = computed(() => props.direct);
const joinable = computed(() => props.available);
const peers = computed(() => props.servers);

/**
 * The modifier this platform actually uses, so the hint is not a lie on one of
 * them. Guarded for the server render, where there is no navigator.
 */
const notifications = useNotifications();
const { branding } = useServerInfo();
const showNotifications = ref(false);
const router = useRouter();

/*
 * The badge, refreshed on a timer.
 *
 * Only the count, not the list: a badge is checked constantly and the list
 * only when somebody opens it, so fetching every notification to draw one
 * digit would be the expensive way round.
 *
 * Thirty seconds because a notification arriving is not urgent — the live
 * connection already delivers messages as they happen, and this is the
 * fallback for everything that is not a message.
 */
onMounted(() => {
  void notifications.refreshCount();

  const timer = setInterval(() => void notifications.refreshCount(), 30_000);
  onBeforeUnmount(() => clearInterval(timer));
});

/** Opens the message a notification is about. */
async function goToNotification(notification: Notification) {
  await router.push({
    path: `/rooms/${notification.roomId}`,
    query: { at: notification.eventId, jump: String(Date.now()) },
  });
}

const shortcut = computed(() => {
  const mac = import.meta.client && /Mac|iP(hone|ad|od)/.test(navigator.platform);
  return mac ? "⌘K" : "Ctrl K";
});

function isCurrent(roomId: string): boolean {
  return route.params.id === roomId;
}

/** The other person's name, rather than the conversation's full title. */
function conversationName(room: Room): string {
  const self = user.value?.displayName;
  const others = room.name
    .split(", ")
    .filter((name) => name !== self)
    .join(", ");
  return others || room.name;
}
</script>

<template>
  <aside class="flex w-60 shrink-0 flex-col border-r bg-surface-sunken">
    <!-- What the operator called this server, applied at runtime rather than
         compiled in. A logo where they set one, their initials otherwise. -->
    <header class="flex h-12 items-center gap-2 border-b px-3">
      <img
        v-if="branding.logoUrl"
        :src="branding.logoUrl"
        :alt="branding.name"
        class="size-6 shrink-0 rounded-md object-cover"
      />
      <span
        v-else
        class="grid size-6 shrink-0 place-items-center rounded-md bg-primary text-[0.625rem] font-bold text-primary-foreground"
      >
        {{ branding.initials }}
      </span>
      <span class="truncate text-sm font-semibold">{{ branding.name }}</span>
    </header>

    <div class="p-2">
      <!-- A button, not a field. It used to filter the rooms already loaded
           here, which answered a different question from the real search and
           found nothing in messages or people — two searches disagreeing is
           worse than one that works. -->
      <button
        type="button"
        data-tour="search"
        class="flex h-8 w-full cursor-pointer items-center gap-2 rounded-lg border border-border bg-muted/40 px-2 text-xs text-muted-foreground transition-colors hover:bg-muted"
        @click="emit('search')"
      >
        <Search class="size-3.5 shrink-0" />
        <span>Search</span>
        <Kbd class="ml-auto">{{ shortcut }}</Kbd>
      </button>
    </div>

    <nav data-tour="channels" class="flex-1 space-y-4 overflow-y-auto px-2 pb-3">
      <section v-if="joined.length">
        <h2
          class="px-2 pb-1 text-[0.6875rem] font-semibold tracking-wide text-muted-foreground uppercase"
        >
          Channels
        </h2>
        <ul class="space-y-px">
          <li v-for="room in joined" :key="room.id">
            <NuxtLink :to="`/rooms/${room.id}`" class="nav-item" :data-active="isCurrent(room.id)">
              <component
                :is="room.visibility === 'private' ? Lock : Hash"
                class="size-3.5 shrink-0 opacity-60"
              />
              <span class="truncate">{{ room.name }}</span>
              <span
                v-if="unread[room.id]"
                class="tabular ml-auto shrink-0 rounded-full bg-primary px-1.5 text-[0.6875rem] font-semibold text-primary-foreground"
              >
                {{ unread[room.id] }}
              </span>
            </NuxtLink>
          </li>
        </ul>
      </section>

      <section v-if="conversations.length">
        <h2
          class="px-2 pb-1 text-[0.6875rem] font-semibold tracking-wide text-muted-foreground uppercase"
        >
          Direct messages
        </h2>
        <ul class="space-y-px">
          <li v-for="room in conversations" :key="room.id">
            <NuxtLink :to="`/rooms/${room.id}`" class="nav-item" :data-active="isCurrent(room.id)">
              <Avatar :name="conversationName(room)" size="sm" class="shrink-0" />
              <span class="truncate">{{ conversationName(room) }}</span>
              <span
                v-if="unread[room.id]"
                class="tabular ml-auto shrink-0 rounded-full bg-primary px-1.5 text-[0.6875rem] font-semibold text-primary-foreground"
              >
                {{ unread[room.id] }}
              </span>
            </NuxtLink>
          </li>
        </ul>
      </section>

      <section v-if="joinable.length">
        <h2
          class="px-2 pb-1 text-[0.6875rem] font-semibold tracking-wide text-muted-foreground uppercase"
        >
          Browse
        </h2>
        <ul class="space-y-px">
          <li v-for="room in joinable" :key="room.id">
            <NuxtLink :to="`/rooms/${room.id}`" class="nav-item" :data-active="isCurrent(room.id)">
              <Hash class="size-3.5 shrink-0 opacity-40" />
              <span class="truncate opacity-75">{{ room.name }}</span>
            </NuxtLink>
          </li>
        </ul>
      </section>

      <section v-if="peers.length">
        <h2
          class="px-2 pb-1 text-[0.6875rem] font-semibold tracking-wide text-muted-foreground uppercase"
        >
          Servers
        </h2>
        <ul class="space-y-px">
          <li v-for="peer in peers" :key="peer.domain">
            <NuxtLink
              :to="`/servers/${peer.domain}`"
              class="nav-item"
              :data-active="route.params.domain === peer.domain"
              :title="peer.description || peer.domain"
            >
              <!-- The icon carries the visibility, the way the channel list
                   uses a lock: a public server is open to the network, a
                   limited or private one is not. -->
              <component
                :is="
                  peer.visibility === 'public'
                    ? Globe
                    : peer.visibility === 'limited'
                      ? Server
                      : Lock
                "
                class="size-3.5 shrink-0 opacity-60"
              />
              <span class="truncate" :class="!peer.reachable && 'opacity-50'">
                {{ peer.name }}
              </span>
              <!-- An unreachable server stays listed, so its absence is
                   visible rather than silent. -->
              <span
                v-if="!peer.reachable"
                class="ml-auto shrink-0 text-[0.6875rem] text-warning"
                title="Not reachable right now"
              >
                offline
              </span>
              <span
                v-else-if="peer.publishedRoomCount"
                class="tabular ml-auto shrink-0 text-[0.6875rem] text-muted-foreground"
              >
                {{ peer.publishedRoomCount }}
              </span>
            </NuxtLink>
          </li>
        </ul>
      </section>

      <p
        v-if="!joined.length && !conversations.length && !joinable.length && !peers.length"
        class="px-2 py-6 text-center text-xs text-muted-foreground"
      >
        No rooms yet.
      </p>
    </nav>

    <div v-if="props.canCreate" data-tour="create" class="border-t p-2">
      <button type="button" class="nav-item w-full" @click="emit('create')">
        <Plus class="size-3.5" />
        New channel
      </button>
    </div>

    <footer class="relative flex items-center gap-2 border-t p-2">
      <Avatar :name="user?.displayName ?? 'You'" size="sm" online />
      <span class="min-w-0 flex-1">
        <span class="block truncate text-xs font-medium">{{ user?.displayName }}</span>
        <span class="block truncate text-[0.6875rem] text-muted-foreground">
          {{ user?.address }}
        </span>
      </span>
      <button
        type="button"
        data-tour="notifications"
        class="btn btn-ghost relative size-7 px-0"
        :aria-label="
          notifications.unread.value
            ? `Notifications, ${notifications.unread.value} unread`
            : 'Notifications'
        "
        @click="showNotifications = !showNotifications"
      >
        <Bell class="size-3.5" />
        <!-- A dot rather than a number past nine: the exact count stops
             meaning anything, and a three-digit badge crowds the footer. -->
        <span
          v-if="notifications.unread.value"
          class="absolute -top-0.5 -right-0.5 grid min-w-4 place-items-center rounded-full bg-primary px-1 text-[0.5625rem] font-semibold text-primary-foreground tabular-nums"
        >
          {{ notifications.unread.value > 9 ? "9+" : notifications.unread.value }}
        </span>
      </button>

      <NotificationPanel v-model="showNotifications" @open="goToNotification" />

      <NuxtLink to="/settings" class="btn btn-ghost size-7 px-0" aria-label="Settings">
        <Settings class="size-3.5" />
      </NuxtLink>
    </footer>
  </aside>
</template>
