<script setup lang="ts">
import type { ServerStatus, Session } from "@tdc/types";
import { isApiError } from "@tdc/types";
import { ArrowLeft, LogOut, Monitor, ScrollText } from "lucide-vue-next";
import { Avatar } from "~/components/ui/avatar";

/** Account settings: identity, status, signed-in devices. */

definePageMeta({ middleware: "auth", layout: "app" });

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

      <section v-if="can('server.audit.read')">
        <h2 class="text-sm font-semibold">Administration</h2>
        <NuxtLink to="/audit" class="btn btn-outline mt-3">
          <ScrollText class="size-3.5" />
          View the audit log
        </NuxtLink>
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
