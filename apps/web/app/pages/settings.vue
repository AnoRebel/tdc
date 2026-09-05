<script setup lang="ts">
import type { ServerStatus, Session } from "@tdc/types";
import { isApiError } from "@tdc/types";
import { ArrowLeft, Flag, LogOut, Monitor, ScrollText, ShieldCheck, UserX } from "lucide-vue-next";
import { Avatar } from "~/components/ui/avatar";

/** Account settings: identity, status, signed-in devices. */

definePageMeta({ middleware: "auth", layout: "app" });

useHead({ title: "Settings" });

const personal = usePersonalModeration();

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
                  class="ml-1.5 rounded bg-success/15 px-1.5 py-0.5 text-[0.6875rem] font-medium text-success"
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
              @click="personal.unblock(person.id)"
            >
              Unblock
            </button>
          </li>
        </ul>
      </section>

      <section v-if="can('server.audit.read') || can('moderation.review')">
        <h2 class="text-sm font-semibold">Administration</h2>
        <div class="mt-3 flex flex-wrap gap-2">
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
</template>
