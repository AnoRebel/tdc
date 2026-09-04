<script setup lang="ts">
import type { LiveEventPayload, Message, Presence, Room, TypingPerson } from "@tdc/types";
import { isApiError } from "@tdc/types";
import {
  Check,
  Hash,
  Lock,
  MessageSquareText,
  Pencil,
  SendHorizontal,
  Trash2,
} from "lucide-vue-next";
import { Avatar } from "~/components/ui/avatar";
import { Bubble } from "~/components/ui/bubble";
import { Kbd } from "~/components/ui/kbd";
import { Message as MessageRow, MessageGroup } from "~/components/ui/message";
import { Separator } from "~/components/ui/separator";
import { Skeleton } from "~/components/ui/skeleton";
import { Spinner } from "~/components/ui/spinner";

definePageMeta({ middleware: "auth", layout: "app" });

const route = useRoute();
const roomId = computed(() => String(route.params.id));

const { user } = useAuth();
const roomsApi = useRooms();
const refreshRooms = inject<() => Promise<void>>("refreshRooms", async () => {});

const room = ref<Room | null>(null);
const messages = ref<Message[]>([]);
const draft = ref("");
const error = ref<string | null>(null);
const loading = ref(true);
const sending = ref(false);
const editing = ref<string | null>(null);
const editDraft = ref("");

const timelineElement = ref<HTMLElement | null>(null);
const composer = ref<HTMLTextAreaElement | null>(null);

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

/**
 * Groups consecutive messages from one sender, so a run reads as one turn.
 *
 * A gap of more than five minutes starts a new group even from the same
 * person: two messages an hour apart are two thoughts, not one.
 */
const grouped = computed(() => {
  const groups: {
    sender: string;
    mine: boolean;
    at: string;
    items: (typeof visible.value)[number][];
  }[] = [];

  for (const message of visible.value) {
    const previous = groups.at(-1);
    const sameSender = previous?.sender === message.sender;
    const soonAfter =
      previous &&
      new Date(message.sentAt).getTime() - new Date(previous.items.at(-1)!.sentAt).getTime() <
        5 * 60 * 1000;

    if (previous && sameSender && soonAfter) {
      previous.items.push(message);
      continue;
    }

    groups.push({
      sender: message.sender,
      mine: message.sender === user.value?.address,
      at: message.sentAt,
      items: [message],
    });
  }

  return groups;
});

/** Date headings between groups, so a long history stays navigable. */
function dayBreakBefore(index: number): string | null {
  const group = grouped.value[index];
  if (!group) return null;

  const day = new Date(group.at).toDateString();
  const previousDay = index > 0 ? new Date(grouped.value[index - 1]!.at).toDateString() : null;
  if (day === previousDay) return null;

  const today = new Date().toDateString();
  const yesterday = new Date(Date.now() - 86_400_000).toDateString();
  if (day === today) return "Today";
  if (day === yesterday) return "Yesterday";
  return new Date(group.at).toLocaleDateString([], {
    weekday: "long",
    day: "numeric",
    month: "long",
  });
}

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
  loading.value = true;
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
  } finally {
    loading.value = false;
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

/** Grows the composer with the message, up to a limit, then scrolls. */
function fitComposer() {
  const element = composer.value;
  if (!element) return;
  element.style.height = "auto";
  element.style.height = `${Math.min(element.scrollHeight, 160)}px`;
}

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
  await nextTick();
  fitComposer();

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
    await nextTick();
    fitComposer();
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
    await refreshRooms();
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
  <div class="flex min-h-0 flex-1">
    <section class="flex min-w-0 flex-1 flex-col">
      <header class="flex h-12 shrink-0 items-center gap-3 border-b px-4">
        <component
          :is="room?.visibility === 'private' ? Lock : Hash"
          class="size-4 shrink-0 text-muted-foreground"
        />

        <div class="min-w-0">
          <h1 class="truncate text-sm leading-tight font-semibold">
            {{ room?.name ?? "Loading" }}
          </h1>
          <p v-if="room?.topic" class="truncate text-xs leading-tight text-muted-foreground">
            {{ room.topic }}
          </p>
        </div>

        <div class="ml-auto flex shrink-0 items-center gap-3 text-xs">
          <span
            v-if="onlineCount > 0"
            class="flex items-center gap-1.5 text-muted-foreground"
            :title="`${onlineCount} online`"
          >
            <span class="size-1.5 rounded-full bg-success" />
            <span class="tabular">{{ onlineCount }}</span>
          </span>

          <!-- Only worth a person's attention when it is not working. -->
          <span
            v-if="live.state.value !== 'connected'"
            class="flex items-center gap-1.5 text-warning"
          >
            <Spinner v-if="live.state.value === 'reconnecting'" size="sm" />
            <span v-else class="size-1.5 rounded-full bg-warning" />
            {{ live.state.value === "reconnecting" ? "Reconnecting" : "Offline" }}
          </span>
        </div>
      </header>

      <p
        v-if="error"
        class="shrink-0 border-b bg-destructive/10 px-4 py-2 text-sm text-destructive"
        role="alert"
      >
        {{ error }}
      </p>

      <div ref="timelineElement" class="flex-1 overflow-y-auto px-4 py-4">
        <!-- Shaped like the messages it stands in for, so the layout does not
             jump when the real thing arrives. -->
        <div v-if="loading" class="space-y-5">
          <div v-for="n in 4" :key="n" class="flex gap-2.5">
            <Skeleton class="size-8 shrink-0 rounded-[0.5rem]" />
            <div class="min-w-0 flex-1 space-y-1.5">
              <Skeleton class="h-3 w-24" />
              <Skeleton class="h-4" :style="{ width: `${45 + ((n * 17) % 45)}%` }" />
            </div>
          </div>
        </div>

        <div v-else-if="grouped.length === 0" class="grid h-full place-items-center text-center">
          <div>
            <span class="mx-auto grid size-10 place-items-center rounded-xl bg-muted">
              <Hash class="size-4 text-muted-foreground" />
            </span>
            <p class="mt-3 text-sm font-medium">This is the start of {{ room?.name }}</p>
            <p class="mt-0.5 text-xs text-muted-foreground">
              {{ room?.member ? "Send the first message." : "Join to take part." }}
            </p>
          </div>
        </div>

        <div v-else class="space-y-3">
          <template v-for="(group, index) in grouped" :key="index">
            <!-- A dated rule, so scrolling back through history stays
                 oriented. -->
            <Separator v-if="dayBreakBefore(index)" :label="dayBreakBefore(index)!" />

            <MessageGroup :mine="group.mine">
              <!-- The name and avatar appear once for the run, on the side the
                   bubbles are on. -->
              <div
                class="flex items-center gap-2 px-0.5"
                :class="group.mine ? 'flex-row-reverse' : 'flex-row'"
              >
                <Avatar :name="group.sender" size="sm" />
                <span class="text-xs font-semibold">
                  {{ group.mine ? "You" : shortName(group.sender) }}
                </span>
                <time class="text-[0.6875rem] text-muted-foreground">{{ when(group.at) }}</time>
              </div>

              <MessageRow
                v-for="(message, position) in group.items"
                :key="message.id"
                :mine="group.mine"
              >
                <!-- Actions sit outside the bubble, on the side away from the
                     edge, and appear only on the row being pointed at. -->
                <div
                  v-if="!message.removed && editing !== message.id"
                  class="flex shrink-0 items-center gap-0.5 opacity-0 transition-opacity focus-within:opacity-100 group-hover/row:opacity-100"
                  :class="group.mine ? 'order-first' : 'order-last'"
                >
                  <button
                    v-if="room?.member"
                    type="button"
                    class="btn btn-ghost size-6 px-0"
                    title="Reply in thread"
                    @click="openThread(message.id)"
                  >
                    <MessageSquareText class="size-3.5" />
                  </button>
                  <button
                    v-if="group.mine"
                    type="button"
                    class="btn btn-ghost size-6 px-0"
                    title="Edit"
                    @click="startEditing(message)"
                  >
                    <Pencil class="size-3.5" />
                  </button>
                  <button
                    v-if="group.mine"
                    type="button"
                    class="btn btn-danger size-6 px-0"
                    title="Delete"
                    @click="remove(message.id)"
                  >
                    <Trash2 class="size-3.5" />
                  </button>
                </div>

                <Bubble
                  :variant="message.removed ? 'redacted' : group.mine ? 'mine' : 'default'"
                  :align="group.mine ? 'end' : 'start'"
                  :continued="position > 0"
                >
                  <form v-if="editing === message.id" @submit.prevent="saveEdit(message.id)">
                    <input
                      v-model="editDraft"
                      class="input h-8"
                      @keydown.esc="editing = null"
                      autofocus
                    />
                    <p class="mt-1 text-[0.6875rem] opacity-70">
                      <Kbd>Enter</Kbd> to save, <Kbd>Esc</Kbd> to cancel
                    </p>
                  </form>

                  <template v-else>
                    {{ message.removed ? "This message was removed" : message.body }}
                    <span
                      v-if="message.edited && !message.removed"
                      class="ml-1 align-baseline text-[0.6875rem] opacity-60"
                    >
                      edited
                    </span>
                  </template>
                </Bubble>
              </MessageRow>

              <button
                v-for="message in group.items.filter((m) => threadCounts[m.id])"
                :key="`thread-${message.id}`"
                type="button"
                class="flex items-center gap-1.5 rounded-md px-1.5 py-0.5 text-xs font-medium text-primary transition hover:bg-accent"
                @click="openThread(message.id)"
              >
                <MessageSquareText class="size-3" />
                {{
                  threadCounts[message.id] === 1 ? "1 reply" : `${threadCounts[message.id]} replies`
                }}
              </button>
            </MessageGroup>
          </template>
        </div>
      </div>

      <footer class="shrink-0 px-4 pt-1 pb-4">
        <p class="h-4 text-xs text-muted-foreground" aria-live="polite">
          {{ typingLabel ? `${typingLabel}…` : "" }}
        </p>

        <form
          v-if="room?.member"
          class="flex items-end gap-2 rounded-xl border bg-surface p-1.5 shadow-xs transition focus-within:border-ring focus-within:shadow-sm"
          @submit.prevent="send"
        >
          <textarea
            ref="composer"
            v-model="draft"
            rows="1"
            :disabled="room.archived"
            :placeholder="room.archived ? 'This room is archived' : `Message ${room.name}`"
            class="max-h-40 flex-1 resize-none bg-transparent px-2 py-1.5 text-sm outline-none placeholder:text-muted-foreground disabled:cursor-not-allowed"
            @input="
              announceTyping();
              fitComposer();
            "
            @keydown.enter.exact.prevent="send"
          />
          <button
            type="submit"
            class="btn btn-primary size-8 shrink-0 px-0"
            :disabled="sending || room.archived || !draft.trim()"
            title="Send"
          >
            <Spinner v-if="sending" size="sm" />
            <SendHorizontal v-else class="size-4" />
          </button>
        </form>

        <div
          v-else-if="room"
          class="flex items-center justify-between gap-4 rounded-xl border bg-surface px-3 py-2.5"
        >
          <p class="text-sm text-muted-foreground">Join this channel to take part.</p>
          <button type="button" class="btn btn-primary" @click="join">
            <Check class="size-3.5" />
            Join
          </button>
        </div>
      </footer>
    </section>

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
