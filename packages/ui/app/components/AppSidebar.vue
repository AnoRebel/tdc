<script setup lang="ts">
import { nameOf } from "#layers/ui/app/lib/names";
import { isMac } from "#layers/ui/app/lib/platform";
import type { ConnectedServer, Room, UnreadCounts } from "@tdc/types";
import type { Notification } from "@tdc/types";
import { Avatar } from "#layers/ui/app/components/ui/avatar";
import { Kbd } from "#layers/ui/app/components/ui/kbd";
import { Button } from "#layers/ui/app/components/ui/button";
import { useIntervalFn, useNow } from "@vueuse/core";
import { appearOfflineKey } from "#layers/ui/app/composables/useOwnStatus";
import { arrangeSidebar, showsUnreadBadge } from "#layers/ui/app/lib/sidebar";
import { presenceDotClass } from "#layers/ui/app/lib/presencedot";
import { remaining } from "#layers/ui/app/lib/statusexpiry";

const { $t, $ts, $localeRoute: localeRoute } = useI18n();

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

const peers = computed(() => props.servers);

/*
 * What is pinned comes out of its usual list and goes to the top, in the
 * order the person chose; everything else stays where it was.
 */
const pins = usePinnedRooms();
const notificationPreferences = useNotificationPreferences();
const groups = computed(() =>
  arrangeSidebar(props.rooms, props.direct, props.available, pins.ids.value),
);
const joined = computed(() => groups.value.channels);
const conversations = computed(() => groups.value.direct);
const joinable = computed(() => groups.value.browse);

function rowProps(room: Room) {
  const setting = notificationPreferences.of(room.id);
  return {
    room,
    unread: props.unread[room.id],
    badge: showsUnreadBadge(
      notificationPreferences.preferences.value,
      room.id,
      props.unread[room.id],
    ),
    muted: room.member && setting.muted,
    current: isCurrent(room.id),
    joinable: !room.member,
  };
}

/* One menu for every row, opened on whichever asked. */
const conversationMenu = useConversationMenu();
const rowMenu = useContextMenu();
const menuFor = ref<Room | null>(null);

async function openRowMenu(room: Room, at: { x: number; y: number } | Element) {
  menuFor.value = room;
  const items = await conversationMenu.itemsFor(room);
  if (at instanceof Element) rowMenu.showFrom(at, items);
  else rowMenu.show(at.x, at.y, items);
}

const newMessage = useNewMessage();
const scheduled = useScheduled();

/*
 * The person's own status, at the foot of the sidebar: a dot for how they
 * appear, and the note with how long it has left. Its menu changes it in one
 * press; the dialog is for a note and a time.
 */
const ownStatus = useOwnStatus();
const statusOpen = useState("status.open", () => false);
const userMenu = useContextMenu();

const now = useNow({ scheduler: (tick) => useIntervalFn(tick, 30_000) });
const statusLine = computed(() => {
  const own = ownStatus.own.value;
  const parts: string[] = [];
  if (own.invisible) parts.push(ownStatus.labelOf(appearOfflineKey));
  else if (own.status !== "available") parts.push(ownStatus.labelOf(own.status));
  if (own.statusText) parts.push(own.statusText);
  if (!parts.length && !own.automaticReply) return null;

  if (own.expiresAt) {
    const left = remaining(new Date(own.expiresAt), now.value);
    if (left) parts.push($ts(`status.left.${left.unit}`, { count: left.count }));
  }
  // Said where the status is, so nobody forgets the server is answering for
  // them — the reason an automatic reply left on is noticed and turned off.
  // First, so a narrow sidebar cuts the status text rather than this.
  if (own.automaticReply) parts.unshift($ts("autoReply.on"));
  return parts.join(" · ");
});

async function openUserMenu(event: MouseEvent) {
  const opener = event.currentTarget as Element;
  const own = ownStatus.own.value;
  const { userMenuItems } = await import("#layers/ui/app/lib/conversationmenu");
  const items = userMenuItems({
    $ts,
    own,
    statuses: ownStatus.statuses.value,
    appearOfflineKey,
    choose: (status) => {
      ownStatus.set({ status, text: own.statusText }).catch(() => say($ts("status.couldNotSave")));
    },
    openDialog: () => (statusOpen.value = true),
    clear: () => {
      ownStatus.clear().catch(() => say($ts("status.couldNotSave")));
    },
    openSettings: () => void router.push(localeRoute({ name: "settings" })),
  });
  userMenu.showFrom(opener, items);
}
const { say } = useNotice();

/**
 * The modifier this platform actually uses, so the hint is not a lie on one of
 * them. Guarded for the server render, where there is no navigator.
 */
const notifications = useNotifications();
const { branding, offers } = useServerInfo();
const showNotifications = ref(false);
const showAppearance = ref(false);
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
  await router.push(
    localeRoute({
      name: "rooms-id",
      params: { id: notification.roomId },
      query: { at: notification.eventId, jump: String(Date.now()) },
    }),
  );
}

const shortcut = computed(() => {
  const mac = import.meta.client && isMac();
  return mac ? "⌘K" : "Ctrl K";
});

function isCurrent(roomId: string): boolean {
  return route.params.id === roomId;
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
        <Icon name="lucide:search" class="size-3.5 shrink-0" />
        <span>{{ $t("nav.search") }}</span>
        <Kbd class="ml-auto">{{ shortcut }}</Kbd>
      </button>
    </div>

    <nav data-tour="channels" class="flex-1 space-y-4 overflow-y-auto px-2 pb-3">
      <!-- The directory, where the server offers one. Asked of the server
           rather than assumed, because it is off unless an operator turned it
           on, and a link to a directory that answers 404 is worse than none. -->
      <section v-if="offers('directory')">
        <ul class="space-y-px">
          <li>
            <NuxtLink
              :to="localeRoute({ name: 'directory' })"
              class="nav-item"
              :data-active="String(route.name ?? '').startsWith('directory')"
            >
              <Icon name="lucide:contact" class="size-3.5 shrink-0 opacity-60" />
              <span class="truncate">{{ $t("directory.nav") }}</span>
            </NuxtLink>
          </li>
        </ul>
      </section>
      <!-- Events: what is coming up, on servers that offer them. -->
      <ul v-if="offers('events')" class="space-y-px">
        <li>
          <NuxtLink
            :to="localeRoute({ name: 'events' })"
            class="nav-item"
            :data-active="route.path.startsWith('/events')"
          >
            <Icon name="lucide:calendar-days" class="size-3.5 shrink-0 opacity-60" />
            <span class="truncate">{{ $t("events.title") }}</span>
          </NuxtLink>
        </li>
      </ul>
      <!-- Projects: where work agreed in these conversations is tracked. -->
      <ul class="space-y-px">
        <li>
          <NuxtLink
            :to="localeRoute({ name: 'projects' })"
            class="nav-item"
            :data-active="route.path.startsWith('/projects')"
          >
            <Icon name="lucide:kanban" class="size-3.5 shrink-0 opacity-60" />
            <span class="truncate">{{ $t("projects.title") }}</span>
          </NuxtLink>
        </li>
        <li>
          <NuxtLink
            :to="localeRoute({ name: 'issues' })"
            class="nav-item"
            :data-active="route.path === '/issues'"
          >
            <Icon name="lucide:circle-user-round" class="size-3.5 shrink-0 opacity-60" />
            <span class="truncate">{{ $t("projects.mine.title") }}</span>
          </NuxtLink>
        </li>
        <li>
          <NuxtLink
            :to="localeRoute({ name: 'issues-search' })"
            class="nav-item"
            :data-active="route.path === '/issues/search'"
          >
            <Icon name="lucide:file-search" class="size-3.5 shrink-0 opacity-60" />
            <span class="truncate">{{ $t("projects.search.title") }}</span>
          </NuxtLink>
        </li>
        <li>
          <NuxtLink
            :to="localeRoute({ name: 'views' })"
            class="nav-item"
            :data-active="route.path.startsWith('/views')"
          >
            <Icon name="lucide:bookmark" class="size-3.5 shrink-0 opacity-60" />
            <span class="truncate">{{ $t("projects.saved.title") }}</span>
          </NuxtLink>
        </li>
        <!-- Only while something is waiting: an empty list is not worth a
             permanent line in the sidebar. -->
        <li v-if="scheduled.scheduled.value.length">
          <button type="button" class="nav-item w-full" @click="scheduled.open()">
            <Icon name="lucide:calendar-clock" class="size-3.5 shrink-0 opacity-60" />
            <span class="truncate">{{ $t("scheduled.title") }}</span>
            <span class="tabular ml-auto shrink-0 text-[0.6875rem] text-muted-foreground">
              {{ scheduled.scheduled.value.length }}
            </span>
          </button>
        </li>
      </ul>

      <!-- What somebody keeps to hand, above everything else. Reordered from
           each row's menu, which a keyboard reaches as easily as a pointer. -->
      <SidebarSection v-if="groups.pinned.length" id="pinned" :title="$ts('conversation.pinned')">
        <TransitionGroup tag="ul" name="motion-list" class="relative space-y-px">
          <SidebarRoom
            v-for="room in groups.pinned"
            :key="room.id"
            v-bind="rowProps(room)"
            @menu="openRowMenu(room, $event)"
          />
        </TransitionGroup>
      </SidebarSection>

      <SidebarSection v-if="joined.length" id="channels" :title="$ts('nav.channels')">
        <ul class="space-y-px">
          <SidebarRoom
            v-for="room in joined"
            :key="room.id"
            v-bind="rowProps(room)"
            @menu="openRowMenu(room, $event)"
          />
        </ul>
      </SidebarSection>

      <!-- Shown even when empty, because the "+" beside its heading is how a
           conversation is started at all. -->
      <SidebarSection id="direct" :title="$ts('nav.directMessages')">
        <template #action>
          <button
            type="button"
            class="mr-1 mb-1 grid size-5 cursor-pointer place-items-center rounded-sm text-muted-foreground hover:bg-accent hover:text-foreground"
            :aria-label="$ts('newMessage.title')"
            :title="$ts('newMessage.title')"
            data-new-message
            @click="newMessage.open()"
          >
            <Icon name="lucide:plus" class="size-3.5" aria-hidden="true" />
          </button>
        </template>
        <ul class="space-y-px">
          <SidebarRoom
            v-for="room in conversations"
            :key="room.id"
            v-bind="rowProps(room)"
            @menu="openRowMenu(room, $event)"
          />
        </ul>
        <p v-if="!conversations.length" class="px-2 py-1 text-xs text-muted-foreground">
          {{ $t("newMessage.noneYet") }}
        </p>
      </SidebarSection>

      <SidebarSection v-if="joinable.length" id="browse" :title="$ts('nav.browse')">
        <ul class="space-y-px">
          <SidebarRoom
            v-for="room in joinable"
            :key="room.id"
            v-bind="rowProps(room)"
            @menu="openRowMenu(room, $event)"
          />
        </ul>
      </SidebarSection>

      <SidebarSection v-if="peers.length" id="servers" :title="$ts('nav.servers')">
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
              <Icon
                :name="
                  peer.visibility === 'public'
                    ? 'lucide:globe'
                    : peer.visibility === 'limited'
                      ? 'lucide:server'
                      : 'lucide:lock'
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
                :title="$ts('error.notReachableRightNow')"
              >
                {{ $t("nav.offline") }}
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
      </SidebarSection>

      <p
        v-if="!joined.length && !conversations.length && !joinable.length && !peers.length"
        class="px-2 py-6 text-center text-xs text-muted-foreground"
      >
        {{ $t("nav.noRoomsYet") }}
      </p>
    </nav>

    <div v-if="props.canCreate" data-tour="create" class="border-t p-2">
      <button type="button" class="nav-item w-full" @click="emit('create')">
        <Icon name="lucide:plus" class="size-3.5" />
        {{ $t("nav.newChannel") }}
      </button>
    </div>

    <footer class="relative flex items-center gap-2 border-t p-2">
      <!-- The person, as a button: their status is changed from here, where
           it is shown, rather than three screens away in settings. -->
      <button
        type="button"
        class="-m-1 flex min-w-0 flex-1 cursor-pointer items-center gap-2 rounded-lg p-1 text-left transition-colors hover:bg-accent"
        aria-haspopup="menu"
        data-user-menu
        :title="statusLine ?? undefined"
        :aria-label="
          statusLine ? $ts('status.menuWithStatus', { status: statusLine }) : $ts('status.menu')
        "
        @click="openUserMenu"
      >
        <span class="relative shrink-0">
          <Avatar :name="user ? nameOf(user) : $ts('room.you')" size="sm" />
          <span
            class="absolute -right-0.5 -bottom-0.5 size-2.5 rounded-full ring-2 ring-surface-sunken transition-colors duration-(--motion-duration-fast)"
            :class="presenceDotClass(ownStatus.own.value.status, ownStatus.own.value.invisible)"
            aria-hidden="true"
          />
        </span>
        <span class="min-w-0 flex-1">
          <span class="block truncate text-xs font-medium">{{ user && nameOf(user) }}</span>
          <span class="flex items-center gap-1 text-[0.6875rem] text-muted-foreground">
            <!-- Visible however much of the line the width cuts off; the
                 words are in the line and the button's name. -->
            <Icon
              v-if="ownStatus.own.value.automaticReply"
              name="lucide:message-square-reply"
              class="size-3 shrink-0 text-primary"
              aria-hidden="true"
              data-automatic-reply-on
            />
            <span class="truncate">{{ statusLine ?? user?.address }}</span>
          </span>
        </span>
      </button>

      <LazyContextMenu
        v-if="userMenu.open.value"
        :items="userMenu.items.value"
        :x="userMenu.position.value.x"
        :y="userMenu.position.value.y"
        :label="$ts('status.menu')"
        @close="userMenu.close"
      />
      <LazyContextMenu
        v-if="rowMenu.open.value"
        :items="rowMenu.items.value"
        :x="rowMenu.position.value.x"
        :y="rowMenu.position.value.y"
        :label="$ts('conversation.optionsFor', { name: menuFor?.name ?? '' })"
        @close="rowMenu.close"
      />
      <Button
        variant="ghost"
        size="icon"
        class="relative"
        data-tour="notifications"
        :label="
          notifications.unread.value
            ? $ts('nav.notificationsUnread', { count: notifications.unread.value })
            : $ts('notification.title')
        "
        @click="showNotifications = !showNotifications"
      >
        <Icon name="lucide:bell" class="size-3.5" />
        <!-- A dot rather than a number past nine: the exact count stops
             meaning anything, and a three-digit badge crowds the footer. -->
        <span
          v-if="notifications.unread.value"
          class="absolute -top-0.5 -right-0.5 grid min-w-4 place-items-center rounded-full bg-primary px-1 text-[0.5625rem] font-semibold text-primary-foreground"
        >
          <!-- The number itself is immediate; what moves is the badge, so a
               new message is noticed without the count becoming unreadable. -->
          <UiCount :value="notifications.unread.value" :max="9" />
        </span>
      </Button>

      <NotificationPanel v-model="showNotifications" @open="goToNotification" />

      <!-- Beside the bell rather than only in settings: somebody trying
           colours wants to see the result immediately. -->
      <ThemeToggle v-model="showAppearance" />

      <NuxtLink
        :to="localeRoute({ name: 'help' })"
        class="btn btn-ghost size-7 px-0"
        :aria-label="$ts('help.nav')"
        :title="$ts('help.nav')"
      >
        <Icon name="lucide:circle-help" class="size-3.5" />
      </NuxtLink>

      <NuxtLink
        :to="localeRoute({ name: 'settings' })"
        class="btn btn-ghost size-7 px-0"
        :aria-label="$ts('nav.settings')"
      >
        <Icon name="lucide:settings" class="size-3.5" />
      </NuxtLink>
    </footer>
  </aside>
</template>
