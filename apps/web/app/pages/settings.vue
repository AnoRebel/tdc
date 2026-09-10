<script setup lang="ts">
import type { ServerStatus, Session } from "@tdc/types";
import { isApiError } from "@tdc/types";
import {
  ArrowLeft,
  Bell,
  Compass,
  Flag,
  LogOut,
  Monitor,
  Moon,
  ScrollText,
  ShieldCheck,
  SlidersHorizontal,
  Sun,
  Users,
  UserX,
} from "lucide-vue-next";
import { Avatar } from "#layers/ui/app/components/ui/avatar";

/** Account settings: identity, status, signed-in devices. */

definePageMeta({ middleware: "auth", layout: "app" });

useHead({ title: "Settings" });

const personal = usePersonalModeration();
const theme = useTheme();
const push = usePushNotifications();
const tour = useTour();

/*
 * The person somebody is unblocking, held until they confirm.
 *
 * Blocking is something somebody did for a reason, and the button sits in a
 * list where a misplaced press is easy. Unblocking is reversible, but not
 * before the person is able to reach them again — which is the part that
 * cannot be taken back.
 */
const unblocking = ref<{ id: string; username: string } | null>(null);

async function confirmUnblock() {
  const person = unblocking.value;
  if (!person) return;

  unblocking.value = null;
  await personal.unblock(person.id);
}

/*
 * Gives the introduction again, from the beginning.
 *
 * Forgotten first, so it starts at step one rather than resuming wherever it
 * was last left — somebody asking to be shown around wants the whole thing,
 * not the tail of it. Leaving settings so the tour has something to point at:
 * every step is anchored to the sidebar and the main view.
 */
async function showTourAgain() {
  tour.forget("chat");
  await navigateTo("/");
}

onMounted(() => void push.refresh());

/*
 * The checkbox writes through rather than binding both ways.
 *
 * A v-model would move the control the instant it is clicked and leave it
 * there whether or not the server agreed. Here it shows what the account says,
 * and only moves once the account says something different.
 */
async function chooseHidePushContent(event: Event) {
  const wanted = (event.target as HTMLInputElement).checked;

  try {
    await push.setHideContent(wanted);
  } catch {
    // Put back, because it never changed.
    (event.target as HTMLInputElement).checked = push.hideContent.value;
  }
}

/*
 * Three choices rather than a switch.
 *
 * A two-way toggle has no way to say "whichever my device says", which is what
 * most people want and what somebody who has never opened this already has.
 */
const themeOptions = [
  { value: "auto" as const, label: "Follow my device", icon: Monitor },
  { value: "light" as const, label: "Light", icon: Sun },
  { value: "dark" as const, label: "Dark", icon: Moon },
];

/*
 * The palettes, described by what they look like rather than named after where
 * they came from — "Congrator" means nothing to somebody who has not used it.
 */
const paletteOptions = [
  { value: "default" as const, label: "Cool", description: "Indigo on tinted grey" },
  { value: "congrator" as const, label: "Warm", description: "Terracotta and amber" },
];

/*
 * The accent, typed rather than picked from a list.
 *
 * Whatever is chosen becomes two shades of itself, one per appearance: light
 * mode draws near-white text on the accent and dark mode near-black, so a
 * single value cannot be readable in both. The hue is kept, so what appears is
 * recognisably the colour that was asked for — only its lightness moves.
 */
const accentDraft = ref("");
const accentRefusal = ref("");

watchEffect(() => {
  accentDraft.value = theme.accent.value;
});

function applyAccent() {
  const { refused } = theme.setAccent(accentDraft.value);
  accentRefusal.value = refused ?? "";
}

function clearAccent() {
  accentDraft.value = "";
  accentRefusal.value = "";
  theme.setAccent("");
}

/*
 * Three settings rather than two.
 *
 * "Anyone" and "nobody" leave out the case most people actually want, which is
 * that colleagues can reach them and strangers cannot.
 */
const directMessageOptions = [
  {
    value: "anyone" as const,
    label: "Anyone",
    detail: "Anybody on this server can start a conversation with you.",
  },
  {
    value: "shared" as const,
    label: "People I share a channel with",
    detail: "Colleagues can reach you; people you have never met cannot.",
  },
  {
    value: "nobody" as const,
    label: "Nobody",
    detail:
      "No new conversations. Moderators can still reach you, so that nobody " +
      "is beyond contact by the people responsible for this server.",
  },
];

onMounted(() => void personal.refresh());

const { user, can, signOut, listSessions, revokeSession } = useAuth();
const api = useApi();
const router = useRouter();

const sessions = ref<Session[]>([]);
const statuses = ref<ServerStatus[]>([]);
const currentStatus = ref("available");
const statusText = ref("");
const error = ref<string | null>(null);
const busy = ref<string | null>(null);

async function load() {
  try {
    sessions.value = await listSessions();
    statuses.value = await api.get<ServerStatus[]>("/api/statuses");
  } catch (cause) {
    error.value = isApiError(cause) ? cause.message : "Could not load your settings.";
  }
}

await load();

async function chooseStatus(key: string) {
  currentStatus.value = key;
  try {
    await api.put("/api/me/status", { status: key, text: statusText.value });
  } catch (cause) {
    error.value = isApiError(cause) ? cause.message : "Could not change your status.";
  }
}

async function endSession(id: string) {
  busy.value = id;
  try {
    await revokeSession(id);
    sessions.value = await listSessions();
  } catch (cause) {
    error.value = isApiError(cause) ? cause.message : "Could not end that session.";
  } finally {
    busy.value = null;
  }
}

async function leave() {
  await signOut();
  await router.push("/signin");
}

function when(value: string): string {
  return new Date(value).toLocaleString();
}

/**
 * Names a device the way its owner would recognise it.
 *
 * A user-agent string is precise and unreadable; someone deciding whether to
 * end a session needs "Chrome on Linux", not a version tuple.
 */
function deviceName(userAgent: string): string {
  if (!userAgent) return "Unknown device";

  const browser = /\bEdg\//.test(userAgent)
    ? "Edge"
    : /\bOPR\//.test(userAgent)
      ? "Opera"
      : /\bFirefox\//.test(userAgent)
        ? "Firefox"
        : /\bChrome\//.test(userAgent)
          ? "Chrome"
          : /\bSafari\//.test(userAgent)
            ? "Safari"
            : /^curl\//.test(userAgent)
              ? "curl"
              : null;

  const platform = /\bAndroid\b/.test(userAgent)
    ? "Android"
    : /\b(iPhone|iPad)\b/.test(userAgent)
      ? "iOS"
      : /\bMac OS X\b/.test(userAgent)
        ? "macOS"
        : /\bWindows\b/.test(userAgent)
          ? "Windows"
          : /\bLinux\b/.test(userAgent)
            ? "Linux"
            : null;

  if (browser && platform) return `${browser} on ${platform}`;
  // Anything unrecognised keeps its original string rather than being
  // flattened to "Unknown": an operator may need to see exactly what connected.
  return browser ?? userAgent;
}
</script>

<template>
  <div class="flex-1 overflow-y-auto">
    <header class="flex h-12 items-center gap-3 border-b px-4">
      <NuxtLink to="/" class="btn btn-ghost size-7 px-0" aria-label="Back">
        <ArrowLeft class="size-4" />
      </NuxtLink>
      <h1 class="text-sm font-semibold">Settings</h1>
    </header>

    <div class="max-w-2xl space-y-8 px-4 py-6">
      <p
        v-if="error"
        class="rounded-md bg-destructive/10 px-3 py-2 text-sm text-destructive"
        role="alert"
      >
        {{ error }}
      </p>

      <section>
        <div class="flex items-center gap-3">
          <Avatar :name="user?.displayName ?? 'You'" size="lg" online />
          <div class="min-w-0">
            <p class="truncate font-medium">{{ user?.displayName }}</p>
            <p class="truncate text-sm text-muted-foreground">{{ user?.address }}</p>
          </div>
        </div>
      </section>

      <section>
        <h2 class="text-sm font-semibold">Status</h2>
        <p class="mt-0.5 text-xs text-muted-foreground">
          What others see beside your name. Your server's administrator chooses which are available.
        </p>

        <div class="mt-3 flex flex-wrap gap-1.5">
          <button
            v-for="status in statuses"
            :key="status.key"
            type="button"
            class="btn"
            :class="currentStatus === status.key ? 'btn-primary' : 'btn-outline'"
            @click="chooseStatus(status.key)"
          >
            {{ status.label }}
          </button>
        </div>
      </section>

      <section>
        <h2 class="flex items-center gap-1.5 text-sm font-semibold">
          <Monitor class="size-3.5 text-muted-foreground" />
          Signed-in devices
        </h2>
        <p class="mt-0.5 text-xs text-muted-foreground">
          Ending a session signs that device out immediately.
        </p>

        <ul class="mt-3 space-y-1.5">
          <li
            v-for="session in sessions"
            :key="session.id"
            class="flex flex-wrap items-center justify-between gap-3 rounded-lg border bg-surface px-3 py-2.5"
          >
            <div class="min-w-0">
              <p class="truncate text-sm">
                {{ deviceName(session.userAgent) }}
                <span
                  v-if="session.current"
                  class="ml-1.5 rounded bg-success/15 px-1.5 py-0.5 text-[0.6875rem] font-medium text-success-text"
                >
                  This device
                </span>
              </p>
              <p class="text-xs text-muted-foreground">
                <time>{{ when(session.lastSeenAt) }}</time>
              </p>
            </div>
            <button
              v-if="!session.current"
              type="button"
              class="btn btn-danger"
              :disabled="busy === session.id"
              @click="endSession(session.id)"
            >
              {{ busy === session.id ? "Ending" : "End session" }}
            </button>
          </li>
        </ul>
      </section>

      <section>
        <h2 class="flex items-center gap-1.5 text-sm font-semibold">
          <Sun class="size-4" />
          Appearance
        </h2>
        <p class="mt-1 text-xs text-muted-foreground">
          Following your device is the default, and changes with it.
        </p>

        <div class="mt-3 flex flex-wrap gap-1.5">
          <button
            v-for="option in themeOptions"
            :key="option.value"
            type="button"
            class="btn"
            :class="theme.preference.value === option.value ? 'btn-primary' : 'btn-outline'"
            @click="theme.set(option.value)"
          >
            <component :is="option.icon" class="size-3.5" />
            {{ option.label }}
          </button>
        </div>

        <h3 class="mt-5 text-xs font-semibold">Colours</h3>
        <p class="mt-1 text-xs text-muted-foreground">Each works in both light and dark.</p>

        <div class="mt-2 flex flex-wrap gap-1.5">
          <button
            v-for="option in paletteOptions"
            :key="option.value"
            type="button"
            class="btn"
            :class="theme.palette.value === option.value ? 'btn-primary' : 'btn-outline'"
            :aria-pressed="theme.palette.value === option.value"
            @click="theme.setPalette(option.value)"
          >
            {{ option.label }}
            <span class="text-[0.6875rem] opacity-70">{{ option.description }}</span>
          </button>
        </div>

        <h3 class="mt-5 text-xs font-semibold">Accent</h3>
        <p class="mt-1 text-xs text-muted-foreground">
          The colour on buttons and links. Adjusted for light and dark so its label stays readable
          in both. Leave it empty for the one this palette comes with.
        </p>

        <div class="mt-2 flex flex-wrap items-start gap-1.5">
          <label class="sr-only" for="accent">Accent colour</label>
          <input
            id="accent"
            v-model="accentDraft"
            type="text"
            class="input w-40"
            placeholder="#4f46e5"
            :aria-invalid="Boolean(accentRefusal)"
            :aria-describedby="accentRefusal ? 'accent-refusal' : undefined"
            @keydown.enter.prevent="applyAccent"
          />
          <button type="button" class="btn btn-outline" @click="applyAccent">Apply</button>
          <button
            v-if="theme.accent.value"
            type="button"
            class="btn btn-ghost"
            @click="clearAccent"
          >
            Reset
          </button>
        </div>

        <!-- The number, not just "that will not work": somebody who is 0.2
             short of readable should be told how far off they are. -->
        <p
          v-if="accentRefusal"
          id="accent-refusal"
          role="alert"
          class="mt-1.5 text-xs text-destructive"
        >
          {{ accentRefusal }}
        </p>
      </section>

      <!-- Only where the server offers it. A control that cannot work is
           worse than none: somebody presses it and concludes the application
           is broken rather than that the feature is not set up here. -->
      <section v-if="push.offered.value && push.supported.value">
        <h2 class="flex items-center gap-1.5 text-sm font-semibold">
          <Bell class="size-4" />
          Notifications
        </h2>
        <p class="mt-1 text-xs text-muted-foreground">
          Told about a message while TDC is closed. Each device is separate.
        </p>

        <p
          v-if="push.error.value"
          class="mt-2 rounded-md bg-destructive/10 px-3 py-2 text-xs text-destructive"
          role="alert"
        >
          {{ push.error.value }}
        </p>

        <button
          v-if="!push.subscribedHere.value"
          type="button"
          class="btn btn-primary mt-3"
          :disabled="push.busy.value"
          @click="push.subscribe()"
        >
          <Bell class="size-3.5" />
          {{ push.busy.value ? "Asking" : "Notify me on this device" }}
        </button>

        <ul v-if="push.devices.value.length" class="mt-3 space-y-1.5">
          <li
            v-for="device in push.devices.value"
            :key="device.id"
            class="flex flex-wrap items-center justify-between gap-3 rounded-lg border bg-surface px-3 py-2.5"
          >
            <div class="min-w-0">
              <p class="truncate text-sm">
                {{ device.device }}
                <span
                  v-if="device.current"
                  class="ml-1.5 rounded bg-success/15 px-1.5 py-0.5 text-[0.6875rem] font-medium text-success-text"
                >
                  This device
                </span>
              </p>
              <p class="text-xs text-muted-foreground">
                <template v-if="device.lastUsedAt">
                  Last notified <time>{{ when(device.lastUsedAt) }}</time>
                </template>
                <template v-else>Not used yet</template>
              </p>
            </div>
            <button
              type="button"
              class="btn btn-ghost h-7 px-2 text-xs"
              :disabled="push.busy.value"
              @click="push.unsubscribe(device.id)"
            >
              Stop
            </button>
          </li>
        </ul>

        <!-- A payload is shown on a lock screen: to whoever holds the phone,
             and to anybody beside them. -->
        <label class="mt-3 flex cursor-pointer items-start gap-2">
          <input
            type="checkbox"
            class="mt-0.5 accent-primary"
            :checked="push.hideContent.value"
            :disabled="push.busy.value"
            @change="chooseHidePushContent"
          />
          <span class="min-w-0">
            <span class="block text-xs font-medium">Keep messages off my lock screen</span>
            <span class="block text-[0.6875rem] text-muted-foreground">
              A notification says something arrived, not what it said.
            </span>
          </span>
        </label>
      </section>

      <section>
        <h2 class="flex items-center gap-1.5 text-sm font-semibold">
          <Compass class="size-4" />
          Getting around
        </h2>
        <p class="mt-1 text-xs text-muted-foreground">
          The short introduction to the application, if you would like it again.
        </p>

        <button type="button" class="btn btn-ghost mt-3" @click="showTourAgain">
          <Compass class="size-3.5" />
          Show me around
        </button>
      </section>

      <section>
        <h2 class="flex items-center gap-1.5 text-sm font-semibold">
          <ShieldCheck class="size-4" />
          Privacy
        </h2>
        <p class="mt-1 text-xs text-muted-foreground">
          Who may start a conversation with you. Existing conversations are unaffected by narrowing
          this.
        </p>

        <div class="mt-3 space-y-1">
          <label
            v-for="option in directMessageOptions"
            :key="option.value"
            class="flex cursor-pointer items-start gap-2 rounded-lg px-2 py-2 transition-colors hover:bg-accent"
          >
            <input
              type="radio"
              :value="option.value"
              :checked="personal.directMessages.value === option.value"
              class="mt-0.5 cursor-pointer accent-primary"
              @change="personal.setDirectMessages(option.value)"
            />
            <span class="min-w-0">
              <span class="block text-sm font-medium">{{ option.label }}</span>
              <span class="block text-xs text-muted-foreground">{{ option.detail }}</span>
            </span>
          </label>
        </div>
      </section>

      <section>
        <h2 class="flex items-center gap-1.5 text-sm font-semibold">
          <UserX class="size-4" />
          Blocked
        </h2>
        <p class="mt-1 text-xs text-muted-foreground">
          People you have blocked cannot start a conversation with you. They are not told, and this
          list is yours alone.
        </p>

        <p v-if="!personal.blocked.value.length" class="mt-3 text-xs text-muted-foreground">
          You have not blocked anybody.
        </p>

        <ul v-else class="mt-3 space-y-1">
          <li
            v-for="person in personal.blocked.value"
            :key="person.id"
            class="flex items-center gap-2 rounded-lg px-2 py-1.5"
          >
            <Avatar :name="person.username" size="sm" />
            <span class="min-w-0 flex-1">
              <span class="block truncate text-sm font-medium">{{ person.displayName }}</span>
              <span class="block truncate text-xs text-muted-foreground">
                @{{ person.username }}
              </span>
            </span>
            <button
              type="button"
              class="btn btn-ghost h-7 px-2 text-xs"
              @click="unblocking = person"
            >
              Unblock
            </button>
          </li>
        </ul>
      </section>

      <section
        v-if="
          can('server.audit.read') ||
          can('moderation.review') ||
          can('server.configure') ||
          can('user.suspend') ||
          can('user.invite')
        "
      >
        <h2 class="text-sm font-semibold">Administration</h2>
        <div class="mt-3 flex flex-wrap gap-2">
          <NuxtLink v-if="can('server.configure')" to="/admin" class="btn btn-outline">
            <SlidersHorizontal class="size-3.5" />
            Server settings
          </NuxtLink>
          <NuxtLink
            v-if="can('user.suspend') || can('user.invite')"
            to="/people"
            class="btn btn-outline"
          >
            <Users class="size-3.5" />
            People
          </NuxtLink>
          <NuxtLink v-if="can('moderation.review')" to="/reports" class="btn btn-outline">
            <Flag class="size-3.5" />
            Reports
            <!-- The count is fetched by the queue itself; a badge here would
                 mean every settings visit reads the moderation queue. -->
          </NuxtLink>
          <NuxtLink v-if="can('server.audit.read')" to="/audit" class="btn btn-outline">
            <ScrollText class="size-3.5" />
            View the audit log
          </NuxtLink>
        </div>
      </section>

      <section class="border-t pt-6">
        <button type="button" class="btn btn-danger" @click="leave">
          <LogOut class="size-3.5" />
          Sign out
        </button>
      </section>
    </div>
  </div>

  <ConfirmDialog
    v-if="unblocking"
    :title="`Unblock @${unblocking.username}?`"
    detail="They will be able to start a conversation with you again. They are not told that they were blocked, or that they no longer are."
    confirm-label="Unblock"
    @cancel="unblocking = null"
    @confirm="confirmUnblock"
  />
</template>
