<script setup lang="ts">
import type { Room, Session } from "@tdc/types";
import { isApiError } from "@tdc/types";
import { Separator } from "~/components/ui/separator";

definePageMeta({ middleware: "auth" });

const { user, can, signOut, listSessions, revokeSession } = useAuth();
const roomsApi = useRooms();
const router = useRouter();

const rooms = ref<Room[]>([]);
const sessions = ref<Session[]>([]);
const error = ref<string | null>(null);
const creating = ref(false);
const newSlug = ref("");
const busy = ref<string | null>(null);

/** Rooms the person has joined, shown first because they are the ones in use. */
const joined = computed(() => rooms.value.filter((room) => room.member));
/** Public rooms they could join but have not. */
const available = computed(() => rooms.value.filter((room) => !room.member));

async function load() {
  try {
    rooms.value = await roomsApi.list();
    sessions.value = await listSessions();
  } catch (cause) {
    error.value = isApiError(cause) ? cause.message : "Could not load your rooms.";
  }
}

await load();

async function createRoom() {
  const slug = newSlug.value.trim();
  if (!slug) return;

  creating.value = true;
  error.value = null;

  try {
    const room = await roomsApi.create({ slug, name: slug });
    newSlug.value = "";
    await router.push(`/rooms/${room.id}`);
  } catch (cause) {
    error.value = isApiError(cause) ? cause.message : "Could not create that room.";
  } finally {
    creating.value = false;
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
</script>

<template>
  <div class="mx-auto max-w-3xl px-4 py-6">
    <header class="flex flex-wrap items-center justify-between gap-4 border-b pb-4">
      <div>
        <p class="font-semibold">{{ user?.displayName }}</p>
        <p class="text-sm text-muted-foreground">{{ user?.address }}</p>
      </div>
      <button
        type="button"
        class="rounded-md border px-3 py-1.5 text-sm hover:bg-accent"
        @click="leave"
      >
        Sign out
      </button>
    </header>

    <p
      v-if="error"
      class="mt-4 rounded-md bg-destructive/10 p-3 text-sm text-destructive"
      role="alert"
    >
      {{ error }}
    </p>

    <section class="mt-6">
      <h2 class="text-lg font-semibold">Your rooms</h2>

      <ul v-if="joined.length" class="mt-3 grid gap-2">
        <li v-for="room in joined" :key="room.id">
          <NuxtLink
            :to="`/rooms/${room.id}`"
            class="flex items-center justify-between rounded-lg border p-3 hover:bg-accent"
          >
            <span>
              <span class="font-medium">{{ room.name }}</span>
              <span v-if="room.topic" class="ml-2 text-sm text-muted-foreground">{{
                room.topic
              }}</span>
              <span class="block text-xs text-muted-foreground">{{ room.address }}</span>
            </span>
            <span v-if="room.archived" class="text-xs text-muted-foreground">Archived</span>
          </NuxtLink>
        </li>
      </ul>

      <p v-else class="mt-3 text-sm text-muted-foreground">You are not in any rooms yet.</p>
    </section>

    <section v-if="available.length" class="mt-6">
      <h2 class="text-lg font-semibold">Rooms you can join</h2>
      <ul class="mt-3 grid gap-2">
        <li v-for="room in available" :key="room.id">
          <NuxtLink
            :to="`/rooms/${room.id}`"
            class="flex items-center justify-between rounded-lg border border-dashed p-3 hover:bg-accent"
          >
            <span>
              <span class="font-medium">{{ room.name }}</span>
              <span class="block text-xs text-muted-foreground">{{ room.address }}</span>
            </span>
            <span class="text-xs text-muted-foreground">Join</span>
          </NuxtLink>
        </li>
      </ul>
    </section>

    <section v-if="can('channel.create')" class="mt-6">
      <h2 class="text-lg font-semibold">Create a room</h2>
      <form class="mt-3 flex gap-2" @submit.prevent="createRoom">
        <input
          v-model="newSlug"
          placeholder="general"
          class="flex-1 rounded-md border bg-background px-3 py-2 text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring"
        />
        <button
          type="submit"
          :disabled="creating || !newSlug.trim()"
          class="rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground disabled:opacity-50"
        >
          Create
        </button>
      </form>
      <p class="mt-2 text-xs text-muted-foreground">
        Letters, digits, underscore, dot and hyphen. This becomes the room's address.
      </p>
    </section>

    <Separator class="mt-8" label="Your account" />

    <section class="mt-2">
      <h2 class="text-lg font-semibold">Signed-in devices</h2>
      <ul class="mt-3 grid gap-2">
        <li
          v-for="session in sessions"
          :key="session.id"
          class="flex flex-wrap items-center justify-between gap-3 rounded-lg border p-3"
        >
          <span class="text-sm">
            <span class="font-medium">{{ session.userAgent || "Unknown client" }}</span>
            <span
              v-if="session.current"
              class="ml-2 rounded-full bg-green-500/15 px-2 py-0.5 text-xs text-green-600 dark:text-green-400"
            >
              This device
            </span>
            <span class="block text-xs text-muted-foreground">
              last active {{ when(session.lastSeenAt) }}
            </span>
          </span>
          <button
            v-if="!session.current"
            type="button"
            :disabled="busy === session.id"
            class="rounded-md border border-destructive px-3 py-1.5 text-xs text-destructive disabled:opacity-50"
            @click="endSession(session.id)"
          >
            {{ busy === session.id ? "Ending…" : "End session" }}
          </button>
        </li>
      </ul>
    </section>

    <section v-if="can('server.audit.read')" class="mt-6">
      <NuxtLink to="/audit" class="text-sm underline underline-offset-4"
        >View the audit log</NuxtLink
      >
    </section>
  </div>
</template>
