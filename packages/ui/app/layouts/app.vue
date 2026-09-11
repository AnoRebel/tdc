<script setup lang="ts">
import type { ConnectedServer, Room, UnreadCounts } from "@tdc/types";
import { isApiError } from "@tdc/types";

import { chatTour } from "#layers/ui/app/lib/chattour";
import { readFailure } from "#layers/ui/app/lib/reachability";

/*
 * lastSeenRoomsKey holds the conversations somebody is in.
 *
 * Only what the sidebar draws. Membership is checked by the server on every
 * request, so a stale list here shows a conversation that will refuse to open
 * rather than granting access to one.
 */
const lastSeenRoomsKey = "tdc.lastseen.rooms";

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
const { $localeRoute: localeRoute } = useI18n();
const { can } = useAuth();

/*
 * The introduction, the first time somebody arrives.
 *
 * Asked for here, with the other composables, and deliberately not further
 * down beside the watcher that offers it. Everything below awaits, and a
 * composable called after an await in a `<script setup>` that suspends has no
 * component instance to inject from — useTour reaches for the transport, so it
 * threw "no API client has been provided" from a layout whose first line had
 * injected one perfectly well. When it is *offered* is the watcher's business;
 * where it is *obtained* has to be before the first await.
 */
const tour = useTour();

const rooms = ref<Room[]>([]);
const direct = ref<Room[]>([]);
const unread = ref<UnreadCounts>({});
const servers = ref<ConnectedServer[]>([]);
/*
 * Whether the channel list is showing on a narrow screen.
 *
 * Provided rather than passed down, because what opens it is a button in a
 * page's own header — the layout owns the drawer, and threading a prop and an
 * event through every page to reach one button would be worse than a value
 * they can ask for.
 */
const navigationOpen = ref(false);
provide("navigationOpen", navigationOpen);

// Closed on every navigation: a drawer still covering the conversation
// somebody just chose is a drawer they have to dismiss twice.
const route = useRoute();
watch(
  () => route.fullPath,
  () => (navigationOpen.value = false),
);

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

    // Kept so that opening the application with no connection shows the
    // conversations somebody is in rather than an empty sidebar, which reads
    // as having been signed out of everything.
    try {
      localStorage.setItem(lastSeenRoomsKey, JSON.stringify({ rooms: all, direct: conversations }));
    } catch {
      // Full or refused. The sidebar simply starts empty next time.
    }
  } catch (cause) {
    /*
     * A server that could not be reached leaves the last known list in place.
     * A refusal does not: the server has answered, and showing conversations
     * somebody may no longer be in would be worse than showing none.
     */
    if (readFailure(cause) === "unreachable") restoreRooms();

    // Navigation is not worth interrupting someone over; the page they are on
    // reports its own failures.
  }
}

/** The conversations this browser last saw, if the server cannot be reached. */
function restoreRooms() {
  try {
    const stored = localStorage.getItem(lastSeenRoomsKey);
    if (!stored) return;

    const parsed = JSON.parse(stored) as { rooms?: Room[]; direct?: Room[] };
    if (parsed.rooms) rooms.value = parsed.rooms;
    if (parsed.direct) direct.value = parsed.direct;
  } catch {
    // Unreadable, so the sidebar stays as it is.
  }
}

const searching = ref(false);
const showingShortcuts = ref(false);

/*
 * What the conversation can do, registered by whichever page is showing.
 *
 * Refs rather than an event: the layout has to call these, and a page that is
 * not a conversation simply never sets them — pressing the key then does
 * nothing, which is the right outcome.
 */
const composerFocus = ref<(() => void) | null>(null);
const threadOpen = ref<(() => void) | null>(null);
provide("composerFocus", composerFocus);
provide("threadOpen", threadOpen);

/*
 * The keyboard, bound once for the whole application.
 *
 * Here rather than in each page because a shortcut that worked on one screen
 * and not another would be one nobody relies on. What the conversation itself
 * needs — focusing the message box, opening a thread — is provided by the page
 * currently showing, which is the only thing that knows what "the last
 * message" means.
 */
useShortcuts({
  search: () => (searching.value = !searching.value),
  reference: () => (showingShortcuts.value = !showingShortcuts.value),

  // Switching channel is search with the channels already in front of you;
  // there is no second palette to write.
  switchChannel: () => (searching.value = true),

  previousChannel: () => moveChannel(-1),
  nextChannel: () => moveChannel(1),

  composer: () => composerFocus.value?.(),
  thread: () => threadOpen.value?.(),
});

await refresh();

/**
 * Offers the introduction to somebody who has not had it.
 *
 * Watched rather than checked on mount, because settings shares this layout:
 * asking to be shown around from there navigates here without mounting
 * anything, so a check that ran once would never see the record change and
 * the button would appear to do nothing.
 *
 * Awaited, because what has been seen is now on the account and the first call
 * fetches it. A Promise is always truthy, so forgetting to await would show
 * the introduction to everybody on every visit.
 */
watch(
  () => tour.changed.value,
  async () => {
    if (!(await tour.offered("chat"))) return;

    await nextTick();

    // Still worth giving by the time we get here: somebody who saw it in
    // another tab meanwhile should not be interrupted by a second copy.
    if (await tour.offered("chat")) void tour.give("chat", chatTour());
  },
  { immediate: true },
);

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
    await router.push(localeRoute({ name: "rooms-id", params: { id: room.id } }));
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

/*
 * Moving between channels in the order the sidebar shows them.
 *
 * The joined ones only: stepping into a channel somebody has not joined would
 * be joining it by accident, which is not what an arrow key should do.
 */
function moveChannel(direction: number) {
  const rooms = joined.value;
  if (!rooms.length) return;

  const current = rooms.findIndex((room) => route.params.id === room.id);
  // From anywhere else, the first channel — a person pressing this from
  // settings means "take me to a conversation".
  const next = current < 0 ? 0 : (current + direction + rooms.length) % rooms.length;

  void router.push(localeRoute({ name: "rooms-id", params: { id: rooms[next]!.id } }));
}

/*
 * Moving between channels, shared with whatever asks.
 *
 * The keyboard and a swipe do the same thing, so they call the same function:
 * two implementations of "next channel" would eventually disagree about what
 * next means.
 */
provide("moveChannel", moveChannel);

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
    <!--
      The sidebar is a column beside the conversation on a wide screen and a
      drawer over it on a narrow one.

      One component in both cases rather than two that drift: what changes is
      where it sits, which is a matter of layout, not of what a sidebar is. On
      a phone it slides in over the conversation because 240 pixels of a
      390-pixel screen is most of the screen — beside the messages there would
      be room for about four words a line.
    -->
    <AppSidebar
      :rooms="joined"
      :direct="direct"
      :available="available"
      :unread="unread"
      :can-create="can('channel.create')"
      :servers="servers"
      class="max-md:fixed max-md:inset-y-0 max-md:left-0 max-md:z-40 max-md:transition-transform max-md:duration-200"
      :class="navigationOpen ? 'max-md:translate-x-0' : 'max-md:-translate-x-full'"
      @create="creating = true"
      @search="searching = true"
    />

    <!--
      The backdrop that dismisses the drawer.

      Only on a narrow screen, and only while it is open: on a wide one the
      sidebar is part of the layout and there is nothing to dismiss.
    -->
    <button
      v-if="navigationOpen"
      type="button"
      class="fixed inset-0 z-30 bg-background/60 backdrop-blur-sm md:hidden"
      aria-label="Close the channel list"
      @click="navigationOpen = false"
    />

    <main class="flex min-w-0 flex-1 flex-col">
      <slot />
    </main>

    <SearchPalette
      v-model="searching"
      @shortcuts="showingShortcuts = true"
      @create-channel="creating = true"
    />

    <ShortcutReference v-model="showingShortcuts" />

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
