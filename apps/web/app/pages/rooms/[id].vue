<script setup lang="ts">
import type { LiveEventPayload, Message, Presence, Room, TypingPerson } from "@tdc/types";
import { isApiError } from "@tdc/types";
import { Bubble } from "~/components/ui/bubble";
import { Kbd } from "~/components/ui/kbd";
import { Message as MessageRow, MessageGroup } from "~/components/ui/message";
import { Separator } from "~/components/ui/separator";
import { Spinner } from "~/components/ui/spinner";

definePageMeta({ middleware: "auth" });

const route = useRoute();
const roomId = computed(() => String(route.params.id));

const { user } = useAuth();
const roomsApi = useRooms();

const room = ref<Room | null>(null);
const messages = ref<Message[]>([]);
const draft = ref("");
const error = ref<string | null>(null);
const sending = ref(false);
const editing = ref<string | null>(null);
const editDraft = ref("");

const timelineElement = ref<HTMLElement | null>(null);

const openThreadId = ref<string | null>(null);
const threadCounts = ref<Record<string, number>>({});
const presenceByUser = ref<Record<string, Presence>>({});
const typingNow = ref<TypingPerson[]>([]);

/** People currently present in this room, for the header count. */
const onlineCount = computed(
  () => Object.values(presenceByUser.value).filter((p) => p.state === "online").length,
);

/** Reads as "Amina is typing" or "Amina and Juma are typing". */
const typingLabel = computed(() => {
  const names = typingNow.value.map((person) => person.displayName);
  if (names.length === 0) return "";
  if (names.length === 1) return `${names[0]} is typing`;
  if (names.length === 2) return `${names[0]} and ${names[1]} are typing`;
  return `${names.length} people are typing`;
});

/**
 * Folds edits and deletions into the messages they refer to.
 *
 * Both are separate events rather than changes to the original, so the
 * original keeps its place and its identity and only what is shown changes.
 */
const visible = computed(() => {
  const byId = new Map<string, Message>();
  const replaced = new Map<string, string>();
  const redacted = new Set<string>();

  for (const message of messages.value) {
    if (message.type === "message.edited" && message.content.replaces) {
      replaced.set(message.content.replaces, message.content.body ?? "");
      continue;
    }
    if (message.type === "message.deleted" && message.content.redacts) {
      redacted.add(message.content.redacts);
      continue;
    }
    byId.set(message.id, message);
  }

  return [...byId.values()]
    .filter((message) => message.type === "message.sent")
    .sort((a, b) => a.sequence - b.sequence)
    .map((message) => ({
      ...message,
      body: replaced.get(message.id) ?? message.content.body ?? "",
      edited: replaced.has(message.id),
      removed: redacted.has(message.id) || message.redacted,
    }));
});

/** Groups consecutive messages from one sender, so a run reads as one turn. */
const grouped = computed(() => {
  const groups: { sender: string; mine: boolean; items: (typeof visible.value)[number][] }[] = [];

  for (const message of visible.value) {
    const previous = groups.at(-1);
    if (previous && previous.sender === message.sender) {
      previous.items.push(message);
      continue;
    }
    groups.push({
      sender: message.sender,
      mine: message.sender === user.value?.address,
      items: [message],
    });
  }

  return groups;
});

const live = useLiveConnection({
  onEvent(eventRoomId, sequence, payload: LiveEventPayload) {
    if (eventRoomId !== roomId.value) return;

    // The sender receives their own message twice: once as the response to
    // their request, once as a live frame.
    if (messages.value.some((message) => message.id === payload.id)) return;

    // A reply belongs to its thread, not the channel. Its arrival still
    // matters: the thread's reply count changes.
    if (payload.threadRoot) {
      void refreshThreadCounts();
      return;
    }

    messages.value.push({
      id: payload.id,
      type: payload.type,
      sender: payload.sender,
      sequence,
      content: payload.content,
      sentAt: new Date(payload.createdAt).toISOString(),
      redacted: payload.redacted,
    });
    scrollToLatest();

    // Arriving messages are read as soon as they are shown, and whoever wrote
    // one has evidently stopped typing.
    markCaughtUp();
    void loadTyping();
  },

  onResync(eventRoomId) {
    // Too far behind to replay: refetch rather than leave a hole.
    if (eventRoomId === roomId.value) void load();
  },
});

async function load() {
  try {
    room.value = await roomsApi.get(roomId.value);
    messages.value = await roomsApi.timeline(roomId.value);
    await Promise.all([loadPresence(), loadTyping()]);
    markCaughtUp();
    // Subscribing from the last sequence means a reconnection replays exactly
    // what was missed rather than repeating what is already shown.
    live.subscribe(roomId.value, messages.value.at(-1)?.sequence);
    await nextTick();
    scrollToLatest();
    void refreshThreadCounts();
  } catch (cause) {
    error.value = isApiError(cause) ? cause.message : "Could not open this room.";
  }
}

async function loadPresence() {
  try {
    const states = await roomsApi.presence(roomId.value);
    presenceByUser.value = Object.fromEntries(states.map((state) => [state.userId, state]));
  } catch {
    // Presence is decoration: failing to load it must not stop the
    // conversation being readable.
  }
}

async function loadTyping() {
  try {
    typingNow.value = await roomsApi.typing(roomId.value);
  } catch {
    // As above.
  }
}

/**
 * Records that everything shown has been read.
 *
 * The read position never moves backwards on the server, so sending it from a
 * view that is behind cannot resurface messages already read elsewhere.
 */
function markCaughtUp() {
  const latest = messages.value.at(-1)?.sequence;
  if (typeof latest === "number") {
    void roomsApi.markRead(roomId.value, latest).catch(() => {
      // Unread state is a convenience; failing to record it is not worth
      // interrupting the person.
    });
  }
}

function scrollToLatest() {
  nextTick(() => {
    const element = timelineElement.value;
    if (element) element.scrollTop = element.scrollHeight;
  });
}

// The server expires an indication on its own, so this only has to be sent
// often enough to keep it alive while someone is actually writing.
const announceTyping = useThrottleFn(() => {
  if (room.value?.member) {
    void roomsApi.startTyping(roomId.value).catch(() => {});
  }
}, 4000);

async function send() {
  const body = draft.value.trim();
  if (!body || sending.value) return;

  sending.value = true;
  error.value = null;

  // A fresh token per message, so a retry after a lost response returns the
  // original rather than posting twice.
  const clientToken = crypto.randomUUID();
  const previous = draft.value;
  draft.value = "";

  try {
    const sent = await roomsApi.send(roomId.value, body, clientToken);
    if (!messages.value.some((message) => message.id === sent.id)) {
      messages.value.push(sent);
      scrollToLatest();
    }
    live.markRead(roomId.value, sent.sequence);
  } catch (cause) {
    // The text is restored, so a failure does not lose what was typed.
    draft.value = previous;
    error.value = isApiError(cause) ? cause.message : "Could not send that message.";
  } finally {
    sending.value = false;
  }
}

function startEditing(message: { id: string; body: string }) {
  editing.value = message.id;
  editDraft.value = message.body;
}

async function saveEdit(messageId: string) {
  const body = editDraft.value.trim();
  if (!body) return;

  try {
    const edit = await roomsApi.edit(roomId.value, messageId, body);
    messages.value.push(edit);
    editing.value = null;
  } catch (cause) {
    error.value = isApiError(cause) ? cause.message : "Could not save that edit.";
  }
}

async function remove(messageId: string) {
  try {
    const deletion = await roomsApi.remove(roomId.value, messageId);
    messages.value.push(deletion);
  } catch (cause) {
    error.value = isApiError(cause) ? cause.message : "Could not delete that message.";
  }
}

async function join() {
  try {
    room.value = await roomsApi.join(roomId.value);
  } catch (cause) {
    error.value = isApiError(cause) ? cause.message : "Could not join this room.";
  }
}

/** Opens a thread beside the channel. */
function openThread(messageId: string) {
  openThreadId.value = messageId;
}

/** Refreshes reply counts after posting in a thread. */
async function refreshThreadCounts() {
  const roots = visible.value.map((message) => message.id);
  const counts: Record<string, number> = {};
  await Promise.all(
    roots.map(async (rootId) => {
      try {
        const thread = await roomsApi.thread(roomId.value, rootId);
        if (thread.replies.length > 0) counts[rootId] = thread.replies.length;
      } catch {
        // A thread that cannot be read is simply not counted.
      }
    }),
  );
  threadCounts.value = counts;
}

function shortName(address: string): string {
  return address.split("@")[0] ?? address;
}

function when(value: string): string {
  return new Date(value).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
}

watch(roomId, load, { immediate: true });
</script>

<template>
  <div class="flex h-screen">
    <div class="flex min-w-0 flex-1 flex-col">
      <header class="flex items-center justify-between gap-4 border-b px-4 py-3">
        <div>
          <h1 class="text-base font-semibold">{{ room?.name ?? "Loading…" }}</h1>
          <p v-if="room?.topic" class="text-xs text-muted-foreground">{{ room.topic }}</p>
          <p v-else-if="room" class="text-xs text-muted-foreground">{{ room.address }}</p>
        </div>

        <div class="flex items-center gap-3 text-xs">
          <span
            :class="[
              'flex items-center gap-1.5',
              live.state.value === 'connected' ? 'text-muted-foreground' : 'text-destructive',
            ]"
          >
            <Spinner v-if="live.state.value === 'reconnecting'" class="size-3" />
            <span
              v-else
              :class="[
                'size-2 rounded-full',
                live.state.value === 'connected' ? 'bg-green-500' : 'bg-destructive',
              ]"
            />
            {{ live.state.value }}
          </span>
          <span v-if="onlineCount > 0" class="flex items-center gap-1.5 text-muted-foreground">
            <span class="size-2 rounded-full bg-green-500" />
            {{ onlineCount }} here
          </span>
          <NuxtLink to="/" class="rounded-md border px-2.5 py-1 hover:bg-accent">Rooms</NuxtLink>
        </div>
      </header>

      <p
        v-if="error"
        class="border-b bg-destructive/10 px-4 py-2 text-sm text-destructive"
        role="alert"
      >
        {{ error }}
      </p>

      <div ref="timelineElement" class="flex-1 space-y-3 overflow-y-auto px-4 py-4">
        <Separator v-if="grouped.length === 0" label="No messages yet" />

        <MessageGroup v-for="(group, index) in grouped" :key="index" :mine="group.mine">
          <p class="px-1 text-xs font-medium text-muted-foreground">
            {{ group.mine ? "You" : shortName(group.sender) }}
          </p>

          <MessageRow v-for="message in group.items" :key="message.id" :mine="group.mine">
            <div class="group flex items-end gap-1.5">
              <div
                v-if="group.mine && !message.removed"
                class="flex gap-1 opacity-0 transition group-hover:opacity-100"
              >
                <button
                  type="button"
                  class="rounded px-1.5 py-0.5 text-xs text-muted-foreground hover:bg-accent"
                  @click="startEditing(message)"
                >
                  Edit
                </button>
                <button
                  type="button"
                  class="rounded px-1.5 py-0.5 text-xs text-destructive hover:bg-accent"
                  @click="remove(message.id)"
                >
                  Delete
                </button>
              </div>

              <Bubble
                :variant="message.removed ? 'redacted' : group.mine ? 'mine' : 'default'"
                :align="group.mine ? 'end' : 'start'"
              >
                <template v-if="editing === message.id">
                  <input
                    v-model="editDraft"
                    class="w-full rounded border bg-background px-2 py-1 text-sm text-foreground"
                    @keydown.enter.prevent="saveEdit(message.id)"
                    @keydown.esc="editing = null"
                  />
                  <span class="mt-1 block text-[0.6875rem] opacity-70">
                    <Kbd>Enter</Kbd> to save, <Kbd>Esc</Kbd> to cancel
                  </span>
                </template>

                <template v-else>
                  {{ message.removed ? "This message was removed" : message.body }}
                  <span class="ml-2 align-baseline text-[0.6875rem] opacity-60">
                    {{ when(message.sentAt) }}<template v-if="message.edited"> · edited</template>
                  </span>
                </template>
              </Bubble>
            </div>
          </MessageRow>

          <button
            v-for="message in group.items.filter((m) => threadCounts[m.id] || !m.removed)"
            v-show="threadCounts[message.id] || room?.member"
            :key="`thread-${message.id}`"
            type="button"
            class="px-1 text-xs text-muted-foreground underline-offset-2 hover:underline"
            @click="openThread(message.id)"
          >
            <template v-if="threadCounts[message.id]">
              {{
                threadCounts[message.id] === 1 ? "1 reply" : `${threadCounts[message.id]} replies`
              }}
            </template>
            <template v-else>Reply in thread</template>
          </button>
        </MessageGroup>
      </div>

      <p v-if="typingLabel" class="px-4 pb-1 text-xs text-muted-foreground" aria-live="polite">
        {{ typingLabel }}…
      </p>

      <footer class="border-t px-4 py-3">
        <form v-if="room?.member" class="flex gap-2" @submit.prevent="send">
          <input
            v-model="draft"
            @input="announceTyping"
            :disabled="room.archived"
            :placeholder="room.archived ? 'This room is archived' : `Message ${room.name}`"
            class="flex-1 rounded-md border bg-background px-3 py-2 text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring"
            autocomplete="off"
          />
          <button
            type="submit"
            :disabled="sending || room.archived || !draft.trim()"
            class="rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground disabled:opacity-50"
          >
            Send
          </button>
        </form>

        <div v-else-if="room" class="flex items-center justify-between gap-4">
          <p class="text-sm text-muted-foreground">Join this room to take part.</p>
          <button
            type="button"
            class="rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground"
            @click="join"
          >
            Join
          </button>
        </div>
      </footer>
    </div>

    <ThreadPanel
      v-if="openThreadId && user"
      :room-id="roomId"
      :root-id="openThreadId"
      :self-address="user.address"
      @close="openThreadId = null"
      @replied="refreshThreadCounts"
    />
  </div>
</template>
