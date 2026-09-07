<script setup lang="ts">
import type { ConnectedServer, Room, UnreadCounts } from "@tdc/types";
import { isApiError } from "@tdc/types";

/**
 * The application shell: sidebar beside content, on every signed-in page.
 *
 * Room and unread state is loaded here rather than in each page, so moving
 * between conversations does not refetch the navigation and the unread badges
 * stay correct wherever you are.
 */

const roomsApi = useRooms();
const api = useApi();
const router = useRouter();
const { can } = useAuth();

const rooms = ref<Room[]>([]);
const direct = ref<Room[]>([]);
const unread = ref<UnreadCounts>({});
const servers = ref<ConnectedServer[]>([]);
const creating = ref(false);
const newSlug = ref("");

/*
 * A disposable chat, whose messages the server removes after a chosen time.
 *
 * The lifetime is fixed when the room is made and disclosed to everybody in
 * it, so nobody says anything under a different expectation. Offered as
 * presets rather than a number: somebody choosing this is thinking "a day",
 * not "86400 seconds".
 */
const newDisposable = ref(false);
const newLifetime = ref(86400);

const lifetimeChoices = [
  { seconds: 3600, label: "An hour" },
  { seconds: 86400, label: "A day" },
  { seconds: 604800, label: "A week" },
  { seconds: 2592000, label: "A month" },
];
const slugInput = ref<HTMLInputElement | null>(null);
const createError = ref<string | null>(null);

// Direct conversations are listed separately, so they are excluded here even
// though the room list returns them alongside channels.
const joined = computed(() => rooms.value.filter((room) => room.member && room.kind !== "direct"));
const available = computed(() =>
  rooms.value.filter((room) => !room.member && room.kind !== "direct"),
);

async function refresh() {
  try {
    const [all, conversations, counts] = await Promise.all([
      roomsApi.list(),
      roomsApi.listDirect(),
      roomsApi.unread(),
    ]);
    rooms.value = all;
    direct.value = conversations;
    unread.value = counts;
  } catch {
    // Navigation is not worth interrupting someone over; the page they are on
    // reports its own failures.
  }
}

await refresh();

/**
 * Loads the servers this one is connected to.
 *
 * Federation is off by default and is a later capability, so a server that
 * does not offer it answers with a refusal or nothing at all. Either way the
 * section stays empty rather than the navigation failing to load.
 */
async function loadServers() {
  try {
    servers.value = await api.get<ConnectedServer[]>("/api/federation/servers");
  } catch {
    servers.value = [];
  }
}

await loadServers();

// Unread counts change as messages arrive elsewhere, so they are refreshed on
// navigation rather than only at load.
watch(() => router.currentRoute.value.fullPath, refresh);

watch(creating, async (open) => {
  if (!open) return;
  await nextTick();
  slugInput.value?.focus();
});

async function createRoom() {
  const slug = newSlug.value.trim();
  if (!slug) return;

  createError.value = null;
  try {
    const room = await roomsApi.create(
      newDisposable.value
        ? { slug, name: slug, kind: "disposable", messageTtlSeconds: newLifetime.value }
        : { slug, name: slug },
    );
    newSlug.value = "";
    newDisposable.value = false;
    creating.value = false;
    await refresh();
    await router.push(`/rooms/${room.id}`);
  } catch (cause) {
    // A refusal names the permission it wanted, which is precise but not a
    // sentence. Someone who cannot create channels needs to be told that,
    // rather than shown the server's vocabulary.
    createError.value = isApiError(cause)
      ? cause.requiredPermission
        ? "You do not have permission to create channels on this server."
        : cause.message
      : "Could not create that channel.";
  }
}

const searching = ref(false);

/**
 * Opens search on the platform's own shortcut.
 *
 * Command on a Mac, Control elsewhere, which is what every application these
 * people already use binds it to. Slash is not bound: this application has a
 * message box on most screens, and stealing a printable character from it
 * would be worse than having one shortcut.
 */
function onShortcut(event: KeyboardEvent) {
  if (event.key !== "k" || !(event.metaKey || event.ctrlKey)) return;
  event.preventDefault();
  searching.value = !searching.value;
}

onMounted(() => window.addEventListener("keydown", onShortcut));
onBeforeUnmount(() => window.removeEventListener("keydown", onShortcut));

// Available to pages that change room state, so the sidebar stays current.
provide("refreshRooms", refresh);
// Lets the sidebar's search box open the palette rather than owning a second,
// weaker search of its own.
provide("openSearch", () => {
  searching.value = true;
});
</script>

<template>
  <div class="flex h-dvh overflow-hidden bg-background">
    <AppSidebar
      :rooms="joined"
      :direct="direct"
      :available="available"
      :unread="unread"
      :can-create="can('channel.create')"
      :servers="servers"
      @create="creating = true"
      @search="searching = true"
    />

    <main class="flex min-w-0 flex-1 flex-col">
      <slot />
    </main>

    <SearchPalette v-model="searching" />

    <!-- Creating a channel is a small, focused task, so it is a slide-over
         rather than a modal that dims the whole application. -->
    <Transition
      enter-active-class="transition duration-200 ease-out"
      enter-from-class="translate-x-full"
      leave-active-class="transition duration-150 ease-in"
      leave-to-class="translate-x-full"
    >
      <aside
        v-if="creating"
        class="fixed inset-y-0 right-0 z-20 w-80 border-l bg-surface p-4 shadow-lg"
      >
        <h2 class="text-sm font-semibold">
          {{ newDisposable ? "New disposable chat" : "New channel" }}
        </h2>
        <p class="mt-1 text-xs text-muted-foreground">
          The name becomes the channel's address, so people on other servers can reach it.
        </p>

        <form class="mt-4 space-y-3" @submit.prevent="createRoom">
          <div>
            <label for="channel-slug" class="mb-1 block text-xs font-medium">Name</label>
            <input
              id="channel-slug"
              ref="slugInput"
              v-model="newSlug"
              class="input"
              placeholder="general"
              autocomplete="off"
            />
            <p class="mt-1 text-[0.6875rem] text-muted-foreground">
              Letters, digits, underscore, dot and hyphen.
            </p>
          </div>

          <label class="flex cursor-pointer items-start gap-2">
            <input v-model="newDisposable" type="checkbox" class="mt-0.5 accent-primary" />
            <span class="min-w-0">
              <span class="block text-xs font-medium">Disposable</span>
              <span class="block text-[0.6875rem] text-muted-foreground">
                Messages are removed after a set time. Everyone in the chat is told before they
                post.
              </span>
            </span>
          </label>

          <div v-if="newDisposable">
            <label for="channel-lifetime" class="mb-1 block text-xs font-medium">
              Messages last
            </label>
            <select id="channel-lifetime" v-model.number="newLifetime" class="input">
              <option
                v-for="choice in lifetimeChoices"
                :key="choice.seconds"
                :value="choice.seconds"
              >
                {{ choice.label }}
              </option>
            </select>
          </div>

          <p v-if="createError" class="text-xs text-destructive" role="alert">
            {{ createError }}
          </p>

          <div class="flex gap-2">
            <button type="submit" class="btn btn-primary" :disabled="!newSlug.trim()">
              Create
            </button>
            <button type="button" class="btn btn-ghost" @click="creating = false">Cancel</button>
          </div>
        </form>
      </aside>
    </Transition>
  </div>
</template>
