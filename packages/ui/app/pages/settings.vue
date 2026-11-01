<script setup lang="ts">
import { nameOf } from "#layers/ui/app/lib/names";
import type { Session } from "@tdc/types";
import { isApiError } from "@tdc/types";
import {
  ArrowLeft,
  Bell,
  CircleUser,
  Compass,
  Ellipsis,
  DatabaseBackup,
  Flag,
  KeyRound,
  LogOut,
  Monitor,
  ScrollText,
  ShieldCheck,
  SlidersHorizontal,
  Sun,
  Users,
  UserX,
} from "lucide-vue-next";
import { Avatar } from "#layers/ui/app/components/ui/avatar";
import { Button } from "#layers/ui/app/components/ui/button";
import { fully } from "#layers/ui/app/lib/dates";
import { presenceDotClass } from "#layers/ui/app/lib/presencedot";

const { $t, $ts, $localeRoute: localeRoute } = useI18n();

/** Account settings: identity, status, signed-in devices. */

definePageMeta({ middleware: "auth", layout: "app" });

useHead({ title: $ts("settings.title") });

const personal = usePersonalModeration();
const tour = useTour();

/*
 * The person somebody is unblocking, held until they confirm.
 *
 * Blocking is something somebody did for a reason, and the button sits in a
 * list where a misplaced press is easy. Unblocking is reversible, but not
 * before the person is able to reach them again — which is the part that
 * cannot be taken back.
 */
const unblocking = ref<{ id: string; username: string; displayName?: string } | null>(null);

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

// Who may ring this person: the same three answers, said about calls.
const callOptions = [
  {
    value: "anyone" as const,
    label: $t("settings.calls.anyone"),
    detail: $t("settings.calls.anyoneHint"),
  },
  {
    value: "shared" as const,
    label: $t("settings.calls.shared"),
    detail: $t("settings.calls.sharedHint"),
  },
  {
    value: "nobody" as const,
    label: $t("settings.calls.nobody"),
    detail: $t("settings.calls.nobodyHint"),
  },
];
const { offers: serverOffers } = useServerInfo();

/*
 * The three details a member list may carry beyond a name.
 *
 * Phrased as what is shown rather than what is hidden, because the control is a
 * checkbox somebody ticks to be seen. Last seen is separate from presence
 * deliberately: "online now" and "last here at 03:12" disclose quite different
 * things, and people who are happy with the first often are not with the second.
 */
const disclosureOptions = computed(() => [
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
  ...(serverOffers("events")
    ? [
        {
          field: "showsEventAttendance" as const,
          label: $t("events.privacy.showsAttendance"),
          detail: $t("events.privacy.showsAttendanceHint"),
        },
      ]
    : []),
]);

onMounted(() => void personal.refresh());

const { user, can, signOut, listSessions, revokeSession } = useAuth();
const router = useRouter();
const route = useRoute();

/*
 * The page's tabs, one subject each.
 *
 * Which is open lives in the address (?tab=), so a link or a guide can send
 * somebody straight to one and the back button undoes a change of tab. Every
 * panel stays mounted and is only hidden, so moving between them loses
 * nothing half-typed and loads nothing twice.
 */
const tabs = computed(() => [
  { name: "profile", label: $ts("settings.tabs.profile"), icon: CircleUser },
  { name: "security", label: $ts("settings.tabs.security"), icon: KeyRound },
  { name: "appearance", label: $ts("settings.appearance"), icon: Sun },
  { name: "notifications", label: $ts("settings.tabs.notifications"), icon: Bell },
  { name: "privacy", label: $ts("settings.privacy"), icon: ShieldCheck },
  { name: "more", label: $ts("settings.tabs.more"), icon: Ellipsis },
]);

const tab = computed(() => {
  const asked = String(route.query.tab ?? "");
  return tabs.value.some((each) => each.name === asked) ? asked : "profile";
});

function open(name: string) {
  if (name !== tab.value) void router.push({ query: { ...route.query, tab: name }, hash: "" });
}

/** Arrow keys, Home and End move between tabs, as they do in any tab list. */
function moveBetweenTabs(event: KeyboardEvent) {
  const names = tabs.value.map((each) => each.name);
  const at = names.indexOf(tab.value);
  const next =
    event.key === "ArrowRight"
      ? names[(at + 1) % names.length]
      : event.key === "ArrowLeft"
        ? names[(at - 1 + names.length) % names.length]
        : event.key === "Home"
          ? names[0]
          : event.key === "End"
            ? names.at(-1)
            : undefined;
  if (!next) return;
  event.preventDefault();
  open(next);
  void nextTick(() => document.getElementById(`settings-tab-${next}`)?.focus());
}

/*
 * A link to a part of the page by its anchor (#location, #translation) opens
 * the tab that part is on, then scrolls to it: an anchor in a hidden tab
 * would otherwise lead nowhere.
 */
watch(
  () => route.hash,
  async (hash) => {
    if (!hash || import.meta.server) return;
    // Both anchors are on parts loaded on their own, which may not be drawn
    // yet: given two seconds to arrive.
    const id = decodeURIComponent(hash.slice(1));
    let target = document.getElementById(id);
    for (let tries = 0; !target && tries < 20; tries++) {
      await new Promise((resolve) => setTimeout(resolve, 100));
      target = document.getElementById(id);
    }
    const panel = target?.closest<HTMLElement>("[data-settings-panel]")?.dataset.settingsPanel;
    if (panel && panel !== tab.value) {
      await router.replace({ query: { ...route.query, tab: panel }, hash });
      await nextTick();
    }
    target?.scrollIntoView({ block: "start" });
  },
  { immediate: true },
);

const sessions = ref<Session[]>([]);
const error = ref<string | null>(null);
const busy = ref<string | null>(null);

/*
 * The status as it stands, and the dialog that changes it — the same one the
 * sidebar opens, so there is one way of setting a status rather than two that
 * disagree about what a note or an expiry is.
 */
const ownStatus = useOwnStatus();
const showingStatus = useState("status.open", () => false);
const typing = useTypingPreference();

async function load() {
  try {
    sessions.value = await listSessions();
  } catch (cause) {
    error.value = isApiError(cause) ? cause.message : $ts("settings.couldNotLoad");
  }
  void typing.load();
}

await load();

async function setTyping(sends: boolean) {
  try {
    await typing.set(sends);
  } catch {
    error.value = $ts("settings.couldNotSave");
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
    error.value = isApiError(cause) ? cause.message : $ts("settings.couldNotEndSession");
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

  if (browser && platform) return $ts("settings.browserOnPlatform", { browser, platform });
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

      <!-- One subject at a time: the page had grown to a dozen screens of
           scrolling. Which tab is open is in the address, so a link can open
           one and the back button returns to the last. -->
      <div
        role="tablist"
        :aria-label="$ts('settings.title')"
        class="-mx-1 flex gap-1 overflow-x-auto border-b px-1 pb-px"
      >
        <button
          v-for="each in tabs"
          :id="`settings-tab-${each.name}`"
          :key="each.name"
          type="button"
          role="tab"
          :aria-selected="tab === each.name"
          :aria-controls="`settings-panel-${each.name}`"
          :tabindex="tab === each.name ? 0 : -1"
          class="-mb-px flex shrink-0 items-center gap-1.5 border-b-2 px-2.5 py-2 text-xs font-medium whitespace-nowrap transition-colors"
          :class="
            tab === each.name
              ? 'border-primary text-foreground'
              : 'border-transparent text-muted-foreground hover:text-foreground'
          "
          @click="open(each.name)"
          @keydown="moveBetweenTabs"
        >
          <component :is="each.icon" class="size-3.5" aria-hidden="true" />
          {{ each.label }}
        </button>
      </div>

      <div
        v-show="tab === 'profile'"
        id="settings-panel-profile"
        role="tabpanel"
        aria-labelledby="settings-tab-profile"
        data-settings-panel="profile"
        class="space-y-8"
      >
        <section>
          <div class="flex items-center gap-3">
            <Avatar :name="user ? nameOf(user) : $ts('room.you')" size="lg" online />
            <div class="min-w-0">
              <p class="truncate font-medium">{{ user && nameOf(user) }}</p>
              <p class="truncate text-sm text-muted-foreground">{{ user?.address }}</p>
            </div>
          </div>
        </section>

        <!-- What you are called, straight under who you are: the display name
             and the username that is half of the address above. -->
        <AccountNames />

        <!-- Where you are, and who may see it. Loaded on its own: the form and
             detection are needed only by somebody who opens this. -->
        <LazyLocationSetting />

        <section>
          <h2 class="text-sm font-semibold">{{ $t("settings.status") }}</h2>
          <p class="mt-0.5 text-xs text-muted-foreground">
            {{ $t("settings.statusHint") }}
          </p>

          <div class="mt-3 flex flex-wrap items-center gap-3">
            <span class="flex min-w-0 items-center gap-2 text-sm">
              <span
                class="size-2.5 shrink-0 rounded-full"
                :class="presenceDotClass(ownStatus.own.value.status, ownStatus.own.value.invisible)"
                aria-hidden="true"
              />
              <span class="truncate">
                {{ ownStatus.labelOf(ownStatus.own.value.status) }}
                <span v-if="ownStatus.own.value.statusText" class="text-muted-foreground">
                  · {{ ownStatus.own.value.statusText }}
                </span>
              </span>
            </span>
            <Button variant="outline" @click="showingStatus = true">
              {{ $t("status.change") }}
            </Button>
          </div>
        </section>

        <!-- What the server says for you while you are away, beside the status
             most replies are tied to. Loaded on its own, so the form and its
             validation cost nothing to anybody who never opens settings. -->
        <LazyAutomaticReplies />
      </div>

      <div
        v-show="tab === 'security'"
        id="settings-panel-security"
        role="tabpanel"
        aria-labelledby="settings-tab-security"
        data-settings-panel="security"
        class="space-y-8"
      >
        <!-- Both are about who can reach this account, and both come before the
             list of devices that already do. The second factor first, since it is
             the one that protects the password everybody already has. -->
        <!-- Before two-step, because this is the one that decides whether a
             forgotten password is recoverable at all. -->
        <EmailAddress />

        <SecondFactorManager />

        <!-- Draws nothing where the server cannot offer passkeys. -->
        <PasskeyManager />

        <!-- A program's way in without a password, scoped by what it is denied. -->
        <ApiTokens />

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
      </div>

      <div
        v-show="tab === 'appearance'"
        id="settings-panel-appearance"
        role="tabpanel"
        aria-labelledby="settings-tab-appearance"
        data-settings-panel="appearance"
        class="space-y-8"
      >
        <section>
          <h2 class="flex items-center gap-1.5 text-sm font-semibold">
            <Sun class="size-4" />
            {{ $t("settings.appearance") }}
          </h2>
          <p class="mt-1 text-xs text-muted-foreground">
            {{ $t("settings.appearanceHint") }}
          </p>

          <!-- Light or dark, contrast, colours and Crystal Neuo's materials, glass
               and accent: the same chooser as the appearance dialog and the
               desktop client's appearance window. -->
          <AppearanceChooser class="mt-3" />
        </section>

        <section class="space-y-3">
          <InterfacePreferences heading />
        </section>
      </div>

      <div
        v-show="tab === 'notifications'"
        id="settings-panel-notifications"
        role="tabpanel"
        aria-labelledby="settings-tab-notifications"
        data-settings-panel="notifications"
        class="space-y-8"
      >
        <!-- What only this kind of device has: notifications through the
             browser's service worker on the web, the window and the updater on
             the desktop. Each client supplies its own; see DeviceSettings. -->
        <DeviceSettings />

        <!-- A summary by email instead of a push per conversation, where the
             server can send mail; it draws nothing where it cannot. -->
        <DigestPreferences />

        <!-- How calls ring on this device, where the server offers calls. -->
        <section v-if="serverOffers('calls')" class="space-y-2">
          <h2 class="text-sm font-semibold">{{ $t("calls.ringtones") }}</h2>
          <p class="text-xs text-muted-foreground">{{ $t("calls.ringtonesHint") }}</p>
          <CallRingtones />
        </section>
      </div>

      <div
        v-show="tab === 'privacy'"
        id="settings-panel-privacy"
        role="tabpanel"
        aria-labelledby="settings-tab-privacy"
        data-settings-panel="privacy"
        class="space-y-8"
      >
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

          <!-- Who may ring you, where the server offers calls. Ringing is the
               more intrusive way of reaching somebody, so it has its own say. -->
          <template v-if="serverOffers('calls')">
            <h3 class="mt-5 text-xs font-semibold">{{ $t("settings.calls.title") }}</h3>
            <p class="mt-1 text-xs text-muted-foreground">{{ $t("settings.calls.hint") }}</p>
            <div class="mt-3 space-y-1" data-call-privacy>
              <label
                v-for="option in callOptions"
                :key="option.value"
                class="flex cursor-pointer items-start gap-2 rounded-lg px-2 py-2 transition-colors hover:bg-accent"
              >
                <input
                  type="radio"
                  name="call-privacy"
                  :value="option.value"
                  :checked="personal.calls.value === option.value"
                  class="mt-0.5 cursor-pointer accent-primary"
                  @change="personal.setCalls({ calls: option.value })"
                />
                <span class="min-w-0">
                  <span class="block text-sm font-medium">{{ option.label }}</span>
                  <span class="block text-xs text-muted-foreground">{{ option.detail }}</span>
                </span>
              </label>
              <label
                class="flex cursor-pointer items-start gap-2 rounded-lg px-2 py-2 transition-colors hover:bg-accent"
              >
                <input
                  type="checkbox"
                  class="mt-0.5 cursor-pointer accent-primary"
                  :checked="personal.callsFromOtherServers.value"
                  @change="
                    personal.setCalls({
                      callsFromOtherServers: !personal.callsFromOtherServers.value,
                    })
                  "
                />
                <span class="min-w-0">
                  <span class="block text-sm font-medium">{{
                    $t("settings.calls.otherServers")
                  }}</span>
                  <span class="block text-xs text-muted-foreground">
                    {{ $t("settings.calls.otherServersHint") }}
                  </span>
                </span>
              </label>
            </div>
          </template>

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

          <!-- Whether others see you writing. Off, this device stops saying so
               and the server ignores any device that still does. -->
          <label
            class="mt-3 flex cursor-pointer items-start gap-2 rounded-lg px-2 py-2 transition-colors hover:bg-accent"
            data-typing-preference
          >
            <input
              type="checkbox"
              class="mt-0.5 cursor-pointer accent-primary"
              :checked="typing.sends.value !== false"
              @change="setTyping(typing.sends.value === false)"
            />
            <span class="min-w-0">
              <span class="block text-sm font-medium">{{ $t("settings.showsTyping") }}</span>
              <span class="block text-xs text-muted-foreground">{{
                $t("settings.showsTypingHint")
              }}</span>
            </span>
          </label>
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
              <Avatar :name="nameOf(person)" size="sm" />
              <span class="min-w-0 flex-1">
                <span class="block truncate text-sm font-medium">{{ nameOf(person) }}</span>
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
      </div>

      <div
        v-show="tab === 'more'"
        id="settings-panel-more"
        role="tabpanel"
        aria-labelledby="settings-tab-more"
        data-settings-panel="more"
        class="space-y-8"
      >
        <!-- Translating messages, where the server offers it. Lazy: the
             provider forms and their validation are needed only here. -->
        <LazyTranslationSettings v-if="serverOffers('translation')" />

        <LazyEventsFeedSettings v-if="serverOffers('events')" />

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
            can('server.restart') ||
            can('server.backup') ||
            can('server.restore') ||
            can('user.suspend') ||
            can('user.invite') ||
            can('role.manage') ||
            can('role.assign')
          "
        >
          <h2 class="text-sm font-semibold">{{ $t("nav.administration") }}</h2>
          <div class="mt-3 flex flex-wrap gap-2">
            <NuxtLink
              v-if="can('server.configure') || can('server.restart')"
              :to="localeRoute({ name: 'admin' })"
              class="btn btn-outline"
            >
              <SlidersHorizontal class="size-3.5" />
              {{ $t("settings.serverSettings") }}
            </NuxtLink>
            <NuxtLink
              v-if="can('server.backup') || can('server.restore')"
              :to="localeRoute({ name: 'backups' })"
              class="btn btn-outline"
            >
              <DatabaseBackup class="size-3.5" />
              {{ $t("operations.title") }}
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
              v-if="can('role.manage') || can('role.assign')"
              :to="localeRoute({ name: 'roles' })"
              class="btn btn-outline"
            >
              <KeyRound class="size-3.5" />
              {{ $t("roles.title") }}
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
      </div>

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
    :title="$ts('settings.unblockTitle', { name: nameOf(unblocking) })"
    :detail="$ts('settings.unblockDetail')"
    :confirm-label="$ts('settings.unblock')"
    @cancel="unblocking = null"
    @confirm="confirmUnblock"
  />
</template>
