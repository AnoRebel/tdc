<script setup lang="ts">
import type { ConnectedServer, Room, UnreadCounts } from "@tdc/types";
import { useForm } from "@tanstack/vue-form";
import { useIntervalFn, useLocalStorage, useMediaQuery, useWindowFocus } from "@vueuse/core";
import type { AnyFieldApi } from "@tanstack/vue-form";
import * as v from "valibot";

import { isApiError } from "@tdc/types";

import { chatTour } from "#layers/ui/app/lib/chattour";
import { readFailure } from "#layers/ui/app/lib/reachability";
import { Button } from "#layers/ui/app/components/ui/button";
import { Select } from "#layers/ui/app/components/ui/select";
import { Input } from "#layers/ui/app/components/ui/input";
import { Field } from "#layers/ui/app/components/ui/field";
import { Checkbox } from "#layers/ui/app/components/ui/checkbox";

// Calls, on servers that offer them: rings arrive and calls go on wherever
// somebody is. Loaded after the page, never part of the first download.
const { offers: offersCapability } = useServerInfo();
const callsOffered = computed(() => offersCapability("calls"));
const eventsOffered = computed(() => offersCapability("events"));

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
const { $t, $ts, $localeRoute: localeRoute } = useI18n();
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

/*
 * Whether the new-channel panel is open, and whether the shortcut reference is.
 *
 * Shared state for the same reason the search palette is: on the desktop these
 * are menu items, and the menu is rendered above this layout where a local ref
 * cannot be reached.
 */
const creating = useState("channel.creating", () => false);

/*
 * A disposable chat, whose messages the server removes after a chosen time.
 *
 * The lifetime is fixed when the room is made and disclosed to everybody in
 * it, so nobody says anything under a different expectation. Offered as
 * presets rather than a number: somebody choosing this is thinking "a day",
 * not "86400 seconds".
 */

const lifetimeChoices = computed(() => [
  { seconds: 3600, label: $ts("channel.lifetime.hour") },
  { seconds: 86400, label: $ts("channel.lifetime.day") },
  { seconds: 604800, label: $ts("channel.lifetime.week") },
  { seconds: 2592000, label: $ts("channel.lifetime.month") },
]);
const slugInput = ref<HTMLInputElement | null>(null);
const createError = ref<string | null>(null);

/*
 * What a channel needs before it is worth asking the server for.
 *
 * The rule under the field said "Letters, digits, underscore, dot and hyphen"
 * and nothing checked it — the name became the channel's address, so a name
 * with a space in it was refused by the server after a round trip, with the
 * server's own vocabulary rather than the sentence already written above the
 * box.
 */
const channel = computed(() =>
  v.object({
    slug: v.pipe(
      v.string(),
      v.trim(),
      v.minLength(1, $ts("channel.needsName")),
      v.regex(/^[a-z0-9_.-]+$/i, $ts("channel.slugRule")),
    ),
    disposable: v.boolean(),
    lifetime: v.number(),
  }),
);

/** The first thing wrong with a field, once somebody has touched it. */
function firstError(field: AnyFieldApi): string | undefined {
  if (!field.state.meta.isTouched) return undefined;
  const [issue] = field.state.meta.errors;
  if (!issue) return undefined;
  return typeof issue === "string" ? issue : (issue as { message?: string }).message;
}

// Direct conversations are listed separately, so they are excluded here even
// though the room list returns them alongside channels.
const joined = computed(() => rooms.value.filter((room) => room.member && room.kind !== "direct"));
const available = computed(() =>
  rooms.value.filter((room) => !room.member && room.kind !== "direct"),
);

/*
 * What this person has chosen for themselves — pins, mutes, their status —
 * read when the application opens and again whenever its window comes back
 * into focus, which is when another device's changes are worth catching up
 * on. Not on every navigation: none of it changes by moving between
 * conversations. Each keeps its last answer when it cannot get a new one.
 */
const pinnedRooms = usePinnedRooms();
const notificationPreferences = useNotificationPreferences();
const scheduledMessages = useScheduled();
const ownStatus = useOwnStatus();

function refreshPersonal() {
  void pinnedRooms.refresh();
  void notificationPreferences.refresh();
  void ownStatus.refresh();
  void scheduledMessages.refresh();
}
refreshPersonal();
const focused = useWindowFocus();
watch(focused, (isFocused) => isFocused && refreshPersonal());

/*
 * Scheduled messages leave the list as they are sent, which happens on the
 * server's timer rather than in answer to anything here. Looked at again each
 * minute while there is anything to look at, and not at all otherwise.
 */
useIntervalFn(() => {
  if (scheduledMessages.scheduled.value.length) void scheduledMessages.refresh();
}, 60_000);

/*
 * The status lapses, and an automatic reply's window closes, on the server's
 * clock too — so while either could change by itself, the foot of the
 * sidebar asks again each minute rather than going on saying "automatic reply
 * on" for one that ended hours ago.
 */
useIntervalFn(() => {
  const own = ownStatus.own.value;
  if (own.automaticReply || own.expiresAt) void ownStatus.refresh();
}, 60_000);

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

/*
 * Whether the search palette is open.
 *
 * Shared state rather than a local ref, because something outside this layout
 * has to be able to open it: on the desktop the menu bar owns Ctrl+K — GTK
 * claims the accelerator before the webview sees the keystroke — so the menu
 * emits an event that the application root turns into this. A ref here would
 * be unreachable from there, which is why the menu item did nothing.
 */
const searching = useState("search.open", () => false);
const showingShortcuts = useState("shortcuts.open", () => false);

/*
 * Whether the interface preferences are open, from the palette or the
 * shortcut list. Shared state for the same reason the search palette is.
 */
const showingInterface = useState("interface.open", () => false);

/*
 * Whether each has ever been opened, so it is fetched on first use and then
 * kept — staying mounted is what lets it animate out as well as in.
 */
const opened = reactive({
  // The palette too: nobody needs it to see the application, only once they
  // reach for it, and it is one of the larger things the layout carried.
  search: false,
  shortcuts: false,
  interface: false,
  newMessage: false,
  status: false,
  scheduled: false,
});
const newMessage = useNewMessage();
const showingStatus = useState("status.open", () => false);
const showingScheduled = useScheduled().showing;
watchEffect(() => {
  if (searching.value) opened.search = true;
  if (showingShortcuts.value) opened.shortcuts = true;
  if (showingInterface.value) opened.interface = true;
  if (newMessage.state.value.open) opened.newMessage = true;
  if (showingStatus.value) opened.status = true;
  if (showingScheduled.value.open) opened.scheduled = true;
});
const { notice } = useNotice();

/*
 * Whether the sidebar is put away on a wide screen.
 *
 * Kept in this browser: somebody who wants the conversation full width wants
 * it next time too. On a narrow screen the sidebar is a drawer already, and
 * the same key opens and closes that instead.
 */
const sidebarHidden = useLocalStorage("tdc-sidebar-hidden", false, { writeDefaults: false });
const wide = useMediaQuery("(min-width: 768px)");

function toggleSidebar() {
  if (wide.value) sidebarHidden.value = !sidebarHidden.value;
  else navigationOpen.value = !navigationOpen.value;
}

/**
 * Goes somewhere by route name, if that somewhere exists here.
 *
 * The desktop client shares this layout and has no projects or directory
 * screens yet; there the key is left alone rather than navigating to nothing.
 */
function go(name: string, params?: Record<string, string>) {
  if (!router.hasRoute(name)) return false;
  void router.push(localeRoute({ name, params }));
}

/*
 * The keyboard, bound once for the whole application.
 *
 * Here rather than in each page because a shortcut that worked on one screen
 * and not another would be one nobody relies on. What a particular screen
 * means by a key — focusing its message box, opening a thread, filing an
 * issue — is registered by that screen with useShortcutAction, because only
 * it knows what "the last message" or "this project" is.
 */
useShortcuts({
  search: () => (searching.value = !searching.value),
  reference: () => (showingShortcuts.value = !showingShortcuts.value),

  // Switching channel is search with the channels already in front of you;
  // there is no second palette to write.
  switchConversation: () => (searching.value = true),

  previousConversation: () => moveChannel(-1),
  nextConversation: () => moveChannel(1),
  previousUnread: () => moveToUnread(-1),
  nextUnread: () => moveToUnread(1),

  sidebar: toggleSidebar,

  goConversations: () => go("index"),
  goProjects: () => go("projects"),
  goIssues: () => go("issues"),
  goSettings: () => go("settings"),
  goDirectory: () => go("directory"),
  goBoard: () => {
    // The board of the project somebody is in: from its own pages by its key,
    // and from an issue by the key its reference starts with.
    const key =
      typeof route.params.key === "string"
        ? route.params.key
        : typeof route.params.ref === "string"
          ? route.params.ref.split("-")[0]
          : undefined;
    if (!key) return false;
    return go("projects-key-board", { key });
  },

  /*
   * Escape, once everything nearer the focus has had it.
   *
   * A modal dialog closes itself. Otherwise somebody in a text field leaves
   * it, and somebody who is not is closing whatever this layout has open.
   */
  escape: (event) => {
    if (document.querySelector('[aria-modal="true"]')) return false;

    const target = event.target as HTMLElement | null;
    if (target && (target.matches("input, textarea, select") || target.isContentEditable)) {
      target.blur();
      return;
    }
    if (creating.value) return void (creating.value = false);
    if (navigationOpen.value) return void (navigationOpen.value = false);
    return false;
  },
});

/*
 * The form for a new channel, obtained before anything is awaited.
 *
 * Everything below this suspends, and a composable called after a top-level
 * await has no component instance to inject from — useForm reaches for one,
 * so it would fail the way useTour did.
 */
const createForm = useForm({
  defaultValues: { slug: "", disposable: false, lifetime: 86400 },
  validators: { onSubmit: channel.value },
  async onSubmit({ value }) {
    createError.value = null;
    try {
      const room = await roomsApi.create(
        value.disposable
          ? {
              slug: value.slug,
              name: value.slug,
              kind: "disposable",
              messageTtlSeconds: value.lifetime,
            }
          : { slug: value.slug, name: value.slug },
      );
      createForm.reset();
      creating.value = false;
      await refresh();
      await router.push(localeRoute({ name: "rooms-id", params: { id: room.id } }));
    } catch (cause) {
      // A refusal names the permission it wanted, which is precise but not a
      // sentence. Someone who cannot create channels needs to be told that,
      // rather than shown the server's vocabulary.
      createError.value = isApiError(cause)
        ? cause.requiredPermission
          ? $ts("channel.noPermission")
          : cause.message
        : $ts("channel.couldNotCreate");
    }
  },
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
/**
 * Whether a selector turns up shortly, rather than whether it is there now.
 *
 * The sidebar mounts after this layout, so asking once finds nothing even on
 * the screen the tour is about. A short wait distinguishes "not yet" from "not
 * here", which is the whole question — and answering it wrongly either loses
 * the introduction or shows it somewhere it cannot point at anything.
 */
async function appears(selector: string, within = 2000): Promise<boolean> {
  const deadline = Date.now() + within;

  while (Date.now() < deadline) {
    if (document.querySelector(selector)) return true;
    await new Promise((resume) => requestAnimationFrame(() => resume(undefined)));
  }
  return Boolean(document.querySelector(selector));
}

/**
 * Whether this is a screen the chat introduction is about.
 *
 * The conversation list and a conversation. Not settings, administration,
 * reports or the audit log, which share this layout and its sidebar.
 *
 * Gating on the route rather than on whether the tour's targets are present,
 * which was the first attempt and was wrong: all four live in AppSidebar,
 * which this layout renders on every one of those pages. The elements are
 * always there; it is the screen beside them that differs.
 */
function describesThisScreen(path: string): boolean {
  return path === "/" || path.startsWith("/rooms/");
}

/*
 * And taken down when the screen it describes goes away.
 *
 * Navigation here is client-side, so a tour begun on the conversation screen
 * survives a move to settings — where it then explains a sidebar beside a
 * page it knows nothing about.
 *
 * Withdrawn rather than dismissed: somebody who navigated away has not said
 * they have read it, and should be offered it again.
 */
watch(
  () => route.path,
  (path) => {
    if (!describesThisScreen(path)) tour.withdraw();
  },
);

watch(
  () => tour.changed.value,
  async () => {
    if (!(await tour.offered("chat"))) return;

    await nextTick();

    /*
     * Only where the tour has something to point at.
     *
     * This layout is shared by settings, administration, reports and the
     * audit log, and the chat tour's four steps all name controls that live
     * on the conversation screen. On any other page every selector misses,
     * and driver.js falls back to an empty div at the centre of the screen —
     * which it then marks aria-haspopup on a plain div (a critical axe
     * violation) and traps Tab inside, so a keyboard user on their first
     * visit can reach Next and Close and nothing else.
     *
     * Checking the first step is enough: they are all on the same screen, and
     * a partial match would be a change to the tour rather than a page it was
     * never meant for.
     */
    if (!describesThisScreen(route.path)) return;

    const steps = chatTour($ts);
    // The sidebar mounts after this layout, so the tour's targets are not
    // there the instant this runs even on the screen it is about.
    const anchor = steps[0]?.element;
    if (typeof anchor === "string" && !(await appears(anchor))) return;

    // Still worth giving by the time we get here: somebody who saw it in
    // another tab meanwhile should not be interrupted by a second copy.
    if (await tour.offered("chat")) void tour.give("chat", steps);
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
 * Moving to the next conversation with something unread, in sidebar order.
 *
 * Channels then direct messages, which is the order they are listed. Wraps,
 * so pressing it repeatedly visits every unread conversation and comes back.
 */
function moveToUnread(direction: number) {
  const order = [...joined.value, ...direct.value];
  if (!order.length) return;

  const current = order.findIndex((room) => route.params.id === room.id);
  for (let step = 1; step <= order.length; step++) {
    const index =
      ((((current < 0 ? -1 : current) + direction * step) % order.length) + order.length) %
      order.length;
    const room = order[index]!;
    // Not into a muted conversation: somebody who silenced it asked not to
    // be taken there by what arrives in it.
    if (unread.value[room.id] && !notificationPreferences.of(room.id).muted) {
      void router.push(localeRoute({ name: "rooms-id", params: { id: room.id } }));
      return;
    }
  }
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
      class="max-md:fixed max-md:inset-y-0 max-md:left-0 max-md:z-40 max-md:transition-transform max-md:duration-(--motion-duration-slow) max-md:ease-(--motion-ease-out)"
      :class="[
        navigationOpen ? 'max-md:translate-x-0' : 'max-md:-translate-x-full',
        sidebarHidden && 'md:hidden',
      ]"
      @create="creating = true"
      @search="searching = true"
    />

    <!--
      The backdrop that dismisses the drawer.

      Only on a narrow screen, and only while it is open: on a wide one the
      sidebar is part of the layout and there is nothing to dismiss.
    -->
    <Transition name="motion-fade">
      <button
        v-if="navigationOpen"
        type="button"
        class="fixed inset-0 z-30 bg-background/60 backdrop-blur-sm md:hidden"
        :aria-label="$ts('nav.closeChannelList')"
        @click="navigationOpen = false"
      />
    </Transition>

    <!--
      The way back to a sidebar that was put away, for somebody who did it by
      key and does not remember which. Only on a wide screen, where hiding it
      is possible at all.
    -->
    <Button
      v-if="sidebarHidden"
      variant="outline"
      size="icon"
      class="fixed bottom-3 left-3 z-20 max-md:hidden"
      :label="$ts('interface.showSidebar')"
      @click="sidebarHidden = false"
    >
      <Icon name="lucide:panel-left-open" class="size-4" aria-hidden="true" />
    </Button>

    <main class="flex min-w-0 flex-1 flex-col">
      <slot />
    </main>

    <!-- A call goes on whatever page somebody moves to. -->
    <LazyCallHost v-if="callsOffered" />
    <!-- Reminders of events, at the corner of whatever page this is. -->
    <LazyEventsReminderToast v-if="eventsOffered" />

    <LazySearchPalette
      v-if="opened.search"
      v-model="searching"
      @shortcuts="showingShortcuts = true"
      @create-channel="creating = true"
    />

    <!-- Loaded when first opened: neither is part of what somebody needs to
         see the application, so neither belongs in its first download. -->
    <LazyShortcutReference
      v-if="opened.shortcuts"
      v-model="showingShortcuts"
      @preferences="showingInterface = true"
    />
    <LazyInterfacePreferencesDialog v-if="opened.interface" v-model="showingInterface" />
    <LazyNewMessageDialog v-if="opened.newMessage" />
    <LazyStatusDialog v-if="opened.status" v-model="showingStatus" />
    <LazyScheduledList v-if="opened.scheduled" :rooms="[...joined, ...direct]" />

    <!-- What a menu that has already closed did, or could not do. The live
         region is always there, so what appears in it is announced. -->
    <div role="status" class="fixed bottom-6 left-1/2 z-[60] -translate-x-1/2">
      <Transition name="motion-toast">
        <p
          v-if="notice"
          :key="notice.at"
          class="rounded-full bg-foreground px-4 py-2 text-xs font-medium whitespace-nowrap text-background shadow-lg"
        >
          {{ notice.text }}
        </p>
      </Transition>
    </div>

    <!-- Creating a channel is a small, focused task, so it is a slide-over
         rather than a modal that dims the whole application. -->
    <Transition name="motion-sheet">
      <aside
        v-if="creating"
        class="fixed inset-y-0 right-0 z-20 w-80 border-l bg-surface p-4 shadow-lg"
      >
        <createForm.Field name="disposable">
          <template #default="{ field: disposable }">
            <h2 class="text-sm font-semibold">
              {{ disposable.state.value ? $t("nav.newDisposableChat") : $t("nav.newChannel") }}
            </h2>
          </template>
        </createForm.Field>
        <p class="mt-1 text-xs text-muted-foreground">
          {{ $t("channel.nameBecomesAddress") }}
        </p>

        <form class="mt-4 space-y-3" @submit.prevent="createForm.handleSubmit()">
          <createForm.Field name="slug">
            <template #default="{ field }">
              <Field
                :label="$ts('channel.name')"
                :hint="$ts('channel.slugRule')"
                :error="firstError(field)"
              >
                <template #default="{ id, invalid, describedBy }">
                  <Input
                    :id="id"
                    ref="slugInput"
                    :model-value="field.state.value"
                    :placeholder="$ts('channel.namePlaceholder')"
                    autocomplete="off"
                    :invalid="invalid"
                    :described-by="describedBy"
                    @update:model-value="field.handleChange($event)"
                    @blur="field.handleBlur"
                  />
                </template>
              </Field>
            </template>
          </createForm.Field>

          <createForm.Field name="disposable">
            <template #default="{ field }">
              <label class="flex cursor-pointer items-start gap-2">
                <Checkbox
                  class="mt-0.5"
                  :model-value="field.state.value"
                  @update:model-value="field.handleChange($event)"
                />
                <span class="min-w-0">
                  <span class="block text-xs font-medium">{{ $t("channel.disposable") }}</span>
                  <span class="block text-[0.6875rem] text-muted-foreground">
                    {{ $t("channel.disposableHint") }}
                  </span>
                </span>
              </label>

              <div v-if="field.state.value" class="mt-3">
                <createForm.Field name="lifetime">
                  <template #default="{ field: lifetime }">
                    <Field :label="$ts('channel.messagesLast')">
                      <template #default="{ id }">
                        <Select
                          :id="id"
                          :model-value="lifetime.state.value"
                          :label="$ts('channel.messagesLast')"
                          :options="
                            lifetimeChoices.map((choice) => ({
                              value: choice.seconds,
                              label: choice.label,
                            }))
                          "
                          @update:model-value="
                            $event !== undefined && lifetime.handleChange($event)
                          "
                        />
                      </template>
                    </Field>
                  </template>
                </createForm.Field>
              </div>
            </template>
          </createForm.Field>

          <p v-if="createError" class="text-xs text-destructive" role="alert">
            {{ createError }}
          </p>

          <div class="flex gap-2">
            <Button type="submit">{{ $t("action.create") }}</Button>
            <Button variant="ghost" @click="creating = false">{{ $t("action.cancel") }}</Button>
          </div>
        </form>
      </aside>
    </Transition>
  </div>
</template>
