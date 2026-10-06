<script setup lang="ts">
import type { ServerStatus, Session } from "@tdc/types";
import { isApiError } from "@tdc/types";
import {
  ArrowLeft,
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
import { Button } from "#layers/ui/app/components/ui/button";
import { Input } from "#layers/ui/app/components/ui/input";
import { fully } from "#layers/ui/app/lib/dates";

const { $t, $ts, $localeRoute: localeRoute } = useI18n();

/** Account settings: identity, status, signed-in devices. */

definePageMeta({ middleware: "auth", layout: "app" });

useHead({ title: "Settings" });

const personal = usePersonalModeration();
const theme = useTheme();
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
  await navigateTo(localeRoute({ name: "index" }));
}

/*
 * Three choices rather than a switch.
 *
 * A two-way toggle has no way to say "whichever my device says", which is what
 * most people want and what somebody who has never opened this already has.
 */
const themeOptions = [
  { value: "auto" as const, label: $t("settings.followMyDevice"), icon: Monitor },
  { value: "light" as const, label: $t("settings.light"), icon: Sun },
  { value: "dark" as const, label: $t("settings.dark"), icon: Moon },
];

/*
 * The palettes, described by what they look like rather than named after where
 * they came from — "Congrator" means nothing to somebody who has not used it.
 */
const paletteOptions = [
  {
    value: "default" as const,
    label: $t("settings.paletteCool"),
    description: $t("settings.paletteCoolDescription"),
  },
  {
    value: "congrator" as const,
    label: $t("settings.paletteWarm"),
    description: $t("settings.paletteWarmDescription"),
  },
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
    label: $t("settings.anyone"),
    detail: $t("settings.anyoneHint"),
  },
  {
    value: "shared" as const,
    label: $t("settings.sharedChannel"),
    detail: $t("settings.sharedChannelHint"),
  },
  {
    value: "nobody" as const,
    label: $t("settings.nobody"),
    detail: $t("settings.nobodyHint"),
  },
];

/*
 * The three details a member list may carry beyond a name.
 *
 * Phrased as what is shown rather than what is hidden, because the control is a
 * checkbox somebody ticks to be seen. Last seen is separate from presence
 * deliberately: "online now" and "last here at 03:12" disclose quite different
 * things, and people who are happy with the first often are not with the second.
 */
const disclosureOptions = [
  {
    field: "showsPresence" as const,
    label: $t("settings.showsPresence"),
    detail: $t("settings.showsPresenceHint"),
  },
  {
    field: "showsStatus" as const,
    label: $t("settings.showsStatus"),
    detail: $t("settings.showsStatusHint"),
  },
  {
    field: "showsLastSeen" as const,
    label: $t("settings.showsLastSeen"),
    detail: $t("settings.showsLastSeenHint"),
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

/*
 * The session somebody is ending, held until they confirm.
 *
 * Ending one signs that device out where it stands, and whoever is holding it
 * is not told why — they simply find themselves signed out mid-sentence. That
 * is the right power to have over a device somebody has lost; it is the wrong
 * thing to do by a misplaced press on a list where every row looks alike.
 */
const ending = ref<Session | null>(null);

async function confirmEnding() {
  const session = ending.value;
  if (!session) return;
  ending.value = null;
  await endSession(session.id);
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
  await router.push(localeRoute({ name: "signin" }));
}

function when(value: string): string {
  return fully(value);
}

/**
 * Names a device the way its owner would recognise it.
 *
 * A user-agent string is precise and unreadable; someone deciding whether to
 * end a session needs "Chrome on Linux", not a version tuple.
 */
function deviceName(userAgent: string): string {
  if (!userAgent) return $ts("settings.unknownDevice");

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
      <NuxtLink
        :to="localeRoute({ name: 'index' })"
        class="btn btn-ghost size-7 px-0"
        :aria-label="$ts('action.back')"
      >
        <ArrowLeft class="size-4" />
      </NuxtLink>
      <h1 class="text-sm font-semibold">{{ $t("settings.title") }}</h1>
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
        <h2 class="text-sm font-semibold">{{ $t("settings.status") }}</h2>
        <p class="mt-0.5 text-xs text-muted-foreground">
          {{ $t("settings.statusHint") }}
        </p>

        <div class="mt-3 flex flex-wrap gap-1.5">
          <Button
            v-for="status in statuses"
            :key="status.key"
            :variant="currentStatus === status.key ? 'primary' : 'outline'"
            @click="chooseStatus(status.key)"
          >
            {{ status.label }}
          </Button>
        </div>
      </section>

      <!-- Both are about who can reach this account, and both come before the
           list of devices that already do. The second factor first, since it is
           the one that protects the password everybody already has. -->
      <!-- Before two-step, because this is the one that decides whether a
           forgotten password is recoverable at all. -->
      <EmailAddress />

      <SecondFactorManager />

      <!-- Draws nothing where the server cannot offer passkeys. -->
      <PasskeyManager />

      <section>
        <h2 class="flex items-center gap-1.5 text-sm font-semibold">
          <Monitor class="size-3.5 text-muted-foreground" />
          {{ $t("auth.signedInDevices") }}
        </h2>
        <p class="mt-0.5 text-xs text-muted-foreground">
          {{ $t("auth.endingSessionSignsOut") }}
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
                  {{ $t("auth.thisDevice") }}
                </span>
              </p>
              <p class="text-xs text-muted-foreground">
                <time>{{ when(session.lastSeenAt) }}</time>
              </p>
            </div>
            <Button
              variant="danger"
              v-if="!session.current"
              :disabled="busy === session.id"
              @click="ending = session"
            >
              {{ busy === session.id ? $t("settings.endingSession") : $t("auth.endSession") }}
            </Button>
          </li>
        </ul>
      </section>

      <section>
        <h2 class="flex items-center gap-1.5 text-sm font-semibold">
          <Sun class="size-4" />
          {{ $t("settings.appearance") }}
        </h2>
        <p class="mt-1 text-xs text-muted-foreground">
          {{ $t("settings.appearanceHint") }}
        </p>

        <div class="mt-3 flex flex-wrap gap-1.5">
          <Button
            v-for="option in themeOptions"
            :key="option.value"
            :variant="theme.preference.value === option.value ? 'primary' : 'outline'"
            @click="theme.set(option.value)"
          >
            <component :is="option.icon" class="size-3.5" />
            {{ option.label }}
          </Button>
        </div>

        <h3 class="mt-5 text-xs font-semibold">{{ $t("settings.colours") }}</h3>
        <p class="mt-1 text-xs text-muted-foreground">{{ $t("settings.coloursHint") }}</p>

        <div class="mt-2 flex flex-wrap gap-1.5">
          <Button
            v-for="option in paletteOptions"
            :key="option.value"
            :variant="theme.palette.value === option.value ? 'primary' : 'outline'"
            :aria-pressed="theme.palette.value === option.value"
            @click="theme.setPalette(option.value)"
          >
            {{ option.label }}
            <span class="text-[0.6875rem] opacity-70">{{ option.description }}</span>
          </Button>
        </div>

        <h3 class="mt-5 text-xs font-semibold">{{ $t("settings.accent") }}</h3>
        <p class="mt-1 text-xs text-muted-foreground">
          {{ $t("settings.accentHint") }}
        </p>

        <div class="mt-2 flex flex-wrap items-start gap-1.5">
          <label class="sr-only" for="accent">{{ $t("settings.accentColour") }}</label>
          <Input
            class="w-40"
            id="accent"
            v-model="accentDraft"
            placeholder="#4f46e5"
            :aria-invalid="Boolean(accentRefusal)"
            :aria-describedby="accentRefusal ? 'accent-refusal' : undefined"
            @keydown.enter.prevent="applyAccent"
          />
          <Button variant="outline" @click="applyAccent">{{ $t("settings.apply") }}</Button>
          <Button variant="ghost" v-if="theme.accent.value" @click="clearAccent">
            {{ $t("settings.reset") }}
          </Button>
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

      <section class="space-y-3">
        <InterfacePreferences heading />
      </section>

      <!-- What only this kind of device has: notifications through the
           browser's service worker on the web, the window and the updater on
           the desktop. Each client supplies its own; see DeviceSettings. -->
      <DeviceSettings />

      <!-- A summary by email instead of a push per conversation, where the
           server can send mail; it draws nothing where it cannot. -->
      <DigestPreferences />

      <section>
        <h2 class="flex items-center gap-1.5 text-sm font-semibold">
          <Compass class="size-4" />
          {{ $t("settings.gettingAround") }}
        </h2>
        <p class="mt-1 text-xs text-muted-foreground">
          {{ $t("settings.gettingAroundHint") }}
        </p>

        <Button variant="ghost" class="mt-3" @click="showTourAgain">
          <Compass class="size-3.5" />
          {{ $t("settings.showMeAround") }}
        </Button>
      </section>

      <section>
        <h2 class="flex items-center gap-1.5 text-sm font-semibold">
          <ShieldCheck class="size-4" />
          {{ $t("settings.privacy") }}
        </h2>
        <p class="mt-1 text-xs text-muted-foreground">
          {{ $t("settings.privacyHint") }}
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

        <!-- What a member list says about you. Your name and address are not
             here because they are how a message is attributed and a
             conversation is opened: a list without them is not a member list.
             Anything you turn off is left out of what the server sends, rather
             than sent and hidden. -->
        <h3 class="mt-5 text-xs font-semibold">{{ $t("settings.whatOthersCanSee") }}</h3>
        <p class="mt-1 text-xs text-muted-foreground">
          {{ $t("settings.whatOthersCanSeeHint") }}
        </p>

        <div class="mt-3 space-y-1">
          <label
            v-for="detail in disclosureOptions"
            :key="detail.field"
            class="flex cursor-pointer items-start gap-2 rounded-lg px-2 py-2 transition-colors hover:bg-accent"
          >
            <input
              type="checkbox"
              class="mt-0.5 cursor-pointer accent-primary"
              :checked="personal.disclosure.value[detail.field]"
              @change="
                personal.setDisclosure(detail.field, !personal.disclosure.value[detail.field])
              "
            />
            <span class="min-w-0">
              <span class="block text-sm font-medium">{{ detail.label }}</span>
              <span class="block text-xs text-muted-foreground">{{ detail.detail }}</span>
            </span>
          </label>
        </div>

        <p class="mt-2 text-[0.6875rem] text-muted-foreground">
          {{ $t("settings.moderatorCanSeeDisclosure") }}
        </p>
      </section>

      <section>
        <h2 class="flex items-center gap-1.5 text-sm font-semibold">
          <UserX class="size-4" />
          {{ $t("settings.blocked") }}
        </h2>
        <p class="mt-1 text-xs text-muted-foreground">
          {{ $t("settings.blockedHint") }}
        </p>

        <p v-if="!personal.blocked.value.length" class="mt-3 text-xs text-muted-foreground">
          {{ $t("settings.blockedNobody") }}
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
            <Button variant="ghost" class="h-7 px-2 text-xs" @click="unblocking = person">
              {{ $t("settings.unblock") }}
            </Button>
          </li>
        </ul>
      </section>

      <!-- Integrations: webhooks and the programs pointed at this server. -->
      <section>
        <h2 class="text-sm font-semibold">{{ $t("integrations.title") }}</h2>
        <p class="mt-1 text-xs text-muted-foreground">{{ $t("integrations.settingsHint") }}</p>
        <div class="mt-3 flex flex-wrap gap-2">
          <NuxtLink :to="localeRoute({ name: 'integrations' })" class="btn btn-outline">
            {{ $t("integrations.manage") }}
          </NuxtLink>
          <NuxtLink
            v-if="can('server.applications.manage')"
            :to="localeRoute({ name: 'applications' })"
            class="btn btn-outline"
          >
            {{ $t("integrations.applications") }}
          </NuxtLink>
        </div>
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
        <h2 class="text-sm font-semibold">{{ $t("nav.administration") }}</h2>
        <div class="mt-3 flex flex-wrap gap-2">
          <NuxtLink
            v-if="can('server.configure')"
            :to="localeRoute({ name: 'admin' })"
            class="btn btn-outline"
          >
            <SlidersHorizontal class="size-3.5" />
            {{ $t("settings.serverSettings") }}
          </NuxtLink>
          <NuxtLink
            v-if="can('user.suspend') || can('user.invite')"
            :to="localeRoute({ name: 'people' })"
            class="btn btn-outline"
          >
            <Users class="size-3.5" />
            {{ $t("nav.people") }}
          </NuxtLink>
          <NuxtLink
            v-if="can('moderation.review')"
            :to="localeRoute({ name: 'reports' })"
            class="btn btn-outline"
          >
            <Flag class="size-3.5" />
            {{ $t("moderation.reports") }}
            <!-- The count is fetched by the queue itself; a badge here would
                 mean every settings visit reads the moderation queue. -->
          </NuxtLink>
          <NuxtLink
            v-if="can('server.audit.read')"
            :to="localeRoute({ name: 'audit' })"
            class="btn btn-outline"
          >
            <ScrollText class="size-3.5" />
            {{ $t("settings.viewAuditLog") }}
          </NuxtLink>
        </div>
      </section>

      <section class="border-t pt-6">
        <Button variant="danger" @click="leave">
          <LogOut class="size-3.5" />
          {{ $t("auth.signOut") }}
        </Button>
      </section>
    </div>
  </div>

  <ConfirmDialog
    v-if="ending"
    :title="$ts('settings.endSessionTitle')"
    :detail="$ts('settings.endSessionDetail', { device: deviceName(ending.userAgent) })"
    :confirm-label="$ts('auth.endSession')"
    @cancel="ending = null"
    @confirm="confirmEnding"
  />

  <ConfirmDialog
    v-if="unblocking"
    :title="$ts('settings.unblockTitle', { username: unblocking.username })"
    :detail="$ts('settings.unblockDetail')"
    :confirm-label="$ts('settings.unblock')"
    @cancel="unblocking = null"
    @confirm="confirmUnblock"
  />
</template>
