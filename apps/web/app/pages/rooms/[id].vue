<script setup lang="ts">
import type { LiveEventPayload, Message, Presence, Room, TypingPerson } from "@tdc/types";
import { isApiError } from "@tdc/types";
import type { MenuItem } from "~/components/ContextMenu.vue";
import {
  Check,
  Copy,
  Hash,
  Maximize2,
  MoreVertical,
  Paperclip,
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
const filePicker = ref<HTMLInputElement | null>(null);

// Uploads are held here until the message that carries them is sent, so a
// refusal costs only the transfer and a failed send does not lose the file.
const uploads = useUploads();

/** Files picked but not yet sent, kept so a retry needs no second selection. */
const pending = new Map<string, File>();

// Actions are reachable three ways: hover controls with a pointer, a context
// menu by right-click or long press, and the keyboard. A touch screen has no
// hover, so without the menu those actions would be unreachable on a phone.
const contextMenu = useContextMenu();

/** The message awaiting confirmation, if any. Deleting cannot be undone. */
const pendingDelete = ref<{ id: string; body: string } | null>(null);

// Photographs are reduced before they leave the device. A phone camera produces
// several megabytes, and on metered mobile data sending the original to be
// looked at on a screen is the person's money spent for nothing.
const { resize, presets } = useImageResize();

/**
 * Whether to send the original rather than a reduced copy.
 *
 * Off by default because most sharing is of photographs to be looked at, and
 * on metered data the original costs the sender money for detail nobody sees.
 * Sending it anyway is a deliberate choice — for a screenshot whose text must
 * stay sharp, or a photograph someone else will edit.
 */
const sendOriginal = ref(false);

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
      attachments: message.attachments ?? [],
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
      attachments: payload.attachments,
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
    void refreshThreadCounts();
  } catch (cause) {
    error.value = isApiError(cause) ? cause.message : "Could not open this room.";
  } finally {
    loading.value = false;

    // A search result names the message it wants; anything else opens at the
    // bottom, where a conversation is normally resumed.
    if (typeof route.query.at === "string") {
      jumpTo(route.query.at);
    } else {
      scrollToLatest();
    }
  }
}

/*
 * Jumping to a message must also work when the room is already open.
 *
 * Searching from inside a conversation changes only the query, so the page is
 * never remounted and load() does not run again. The jump counter makes each
 * jump a distinct location, so asking for the same message twice still moves.
 */
watch(
  () => [route.query.at, route.query.jump],
  ([eventId]) => {
    if (typeof eventId === "string" && eventId) jumpTo(eventId);
  },
);

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

/*
 * Runs an action once the timeline is showing the messages it will show.
 *
 * Three separate things can be missing, and each of them broke this on its
 * own. The first load is started by a watcher with immediate: true, so it runs
 * before the template has ever rendered and the ref holds null. The skeleton
 * occupies the timeline while loading is true, so a height read then belongs
 * to placeholder content. And nextTick returns before the browser has laid
 * anything out, so even the right element has the wrong height.
 *
 * Waiting on all three is what makes a room open at its newest message rather
 * than at the top. Bounded rather than a bare retry, so an empty room or a
 * reader who has navigated away does not leave a callback running every frame.
 */
function onTimeline(action: (timeline: HTMLElement) => void) {
  let attempts = 0;

  const attempt = () => {
    const element = timelineElement.value;

    // document.contains matters as much as the null check. A room that was
    // already open leaves the ref pointing at the previous render's element,
    // which is detached: scrolling it succeeds silently and moves nothing on
    // screen, which is why rooms kept opening at the top.
    const ready =
      element &&
      document.contains(element) &&
      !loading.value &&
      element.querySelector('[id^="message-"]');

    if (ready) {
      // setTimeout rather than requestAnimationFrame: rAF is suspended in a
      // background or unfocused tab, so a room restored into one would open at
      // the top and stay there. A zero timeout still lands after the browser
      // has laid the timeline out, and it runs whether or not anything is
      // painting.
      setTimeout(() => action(element), 0);
      return;
    }

    // About a second in total: far longer than a mount and a render take, and
    // still finite when there is nothing to scroll to.
    if (++attempts > 60) return;
    setTimeout(attempt, 16);
  };

  void nextTick(attempt);
}

/**
 * Scrolls to one message and marks it.
 *
 * The highlight matters as much as the scroll: landing in the middle of a
 * conversation with no sign of which message was meant leaves the reader to
 * work it out from search terms they have already stopped looking at. It fades
 * rather than persisting, because it describes how you arrived rather than
 * anything about the message.
 */
function jumpTo(eventId: string) {
  onTimeline((timeline) => {
    const element = timeline.querySelector<HTMLElement>(`[id="message-${CSS.escape(eventId)}"]`);
    if (!element) {
      // The message is real but outside the loaded window — history is not
      // paged in yet. The bottom is a better answer than an unmoved view.
      scrollToLatest();
      return;
    }

    element.scrollIntoView({ block: "center" });
    element.classList.add("just-found");
    setTimeout(() => element.classList.remove("just-found"), 2000);
  });
}

function scrollToLatest() {
  onTimeline((timeline) => {
    timeline.scrollTop = timeline.scrollHeight;
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

/** Starts uploading whatever was picked, without blocking the conversation. */
async function pickFiles(event: Event) {
  const input = event.target as HTMLInputElement;
  const files = [...(input.files ?? [])];
  // Cleared so picking the same file twice in a row still fires a change.
  input.value = "";

  for (const original of files) {
    // Anything that is not a reducible image comes back untouched, so this is
    // safe to apply to everything.
    const file = sendOriginal.value ? original : await resize(original, presets.shared);

    const started = uploads.upload(roomId.value, file, original);
    void started.then((upload) => pending.set(upload.id, file));
  }
}

function retryUpload(id: string) {
  // The already-reduced file, so a retry does not repeat the work or re-send
  // the original.
  const file = pending.get(id);
  if (file) void uploads.retry(roomId.value, id, file);
}

async function send() {
  const body = draft.value.trim();
  const ready = uploads.completed.value;

  // A message may be nothing but its attachments, so an empty body is only a
  // reason to refuse when there is nothing to attach either.
  if ((!body && ready.length === 0) || sending.value) return;

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
    const sent = await roomsApi.send(
      roomId.value,
      body,
      clientToken,
      ready.map((attachment) => attachment.id),
    );
    uploads.clearFinished();
    pending.clear();
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

/**
 * Builds the menu for a message.
 *
 * Only what this person may actually do: offering an action that will be
 * refused is worse than not offering it, because the refusal arrives after
 * they have already decided.
 */
function menuFor(
  message: { id: string; body: string; removed: boolean },
  mine: boolean,
): MenuItem[] {
  const items: MenuItem[] = [];

  if (!message.removed) {
    items.push({
      label: "Copy text",
      icon: Copy,
      action: () => navigator.clipboard?.writeText(message.body),
    });
  }

  if (room.value?.member && !message.removed) {
    items.push({
      label: "Reply in thread",
      icon: MessageSquareText,
      action: () => openThread(message.id),
    });
  }

  if (mine && !message.removed) {
    items.push(
      { label: "Edit", icon: Pencil, action: () => startEditing(message) },
      {
        label: "Delete",
        icon: Trash2,
        destructive: true,
        action: () => confirmDelete(message),
      },
    );
  }

  return items;
}

/** Asks before deleting, since a deleted message cannot be brought back. */
function confirmDelete(message: { id: string; body: string }) {
  pendingDelete.value = { id: message.id, body: message.body };
}

async function remove(messageId: string) {
  pendingDelete.value = null;
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

/*
 * Opens a thread beside the channel, and brings its root into view.
 *
 * The panel shows the message it is about, but the channel behind it stays
 * where it was — so a reply opened from a count near the top left the
 * conversation showing an unrelated part of itself.
 */
function openThread(messageId: string) {
  openThreadId.value = messageId;
  jumpTo(messageId);
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
                :id="`message-${message.id}`"
                :key="message.id"
                :mine="group.mine"
                v-bind="contextMenu.handlers(() => menuFor(message, group.mine))"
              >
                <!-- Actions sit outside the bubble, on the side away from the
                     edge, and appear only on the row being pointed at. -->
                <div
                  v-if="!message.removed && editing !== message.id"
                  class="flex shrink-0 items-center gap-0.5 opacity-0 transition-opacity focus-within:opacity-100 group-hover/row:opacity-100 max-md:opacity-100"
                  :class="group.mine ? 'order-first' : 'order-last'"
                >
                  <!-- A touch screen has no hover, so the controls stay visible
                       there and this opens the same menu a long press would.
                       Without it, editing and deleting would be unreachable on
                       a phone. -->
                  <button
                    type="button"
                    class="btn btn-ghost size-8 px-0 md:hidden"
                    aria-label="Message actions"
                    @click.stop="
                      contextMenu.show(
                        ($event.currentTarget as HTMLElement).getBoundingClientRect().left,
                        ($event.currentTarget as HTMLElement).getBoundingClientRect().bottom,
                        menuFor(message, group.mine),
                      )
                    "
                  >
                    <MoreVertical class="size-4" />
                  </button>
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
                    @click="confirmDelete(message)"
                  >
                    <Trash2 class="size-3.5" />
                  </button>
                </div>

                <div
                  class="flex w-fit max-w-full flex-col gap-1"
                  :class="group.mine ? 'items-end' : 'items-start'"
                >
                  <Bubble
                    v-if="message.body || message.removed"
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

                  <AttachmentList
                    v-if="!message.removed && message.attachments.length"
                    :attachments="message.attachments"
                  />

                  <!-- Only the first link: a message full of them would
                       otherwise produce a wall of cards taller than the
                       conversation. -->
                  <LinkPreview
                    v-if="!message.removed && firstLinkIn(message.body)"
                    :key="firstLinkIn(message.body) ?? ''"
                    :url="firstLinkIn(message.body)!"
                  />
                </div>
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

        <UploadTray
          v-if="room?.member"
          :uploads="uploads.uploads.value"
          @cancel="uploads.cancel"
          @retry="retryUpload"
          @dismiss="uploads.remove"
        />

        <form
          v-if="room?.member"
          class="flex items-end gap-2 rounded-xl border bg-surface p-1.5 shadow-xs transition focus-within:border-ring focus-within:shadow-sm"
          @submit.prevent="send"
        >
          <input ref="filePicker" type="file" multiple class="sr-only" @change="pickFiles" />
          <button
            type="button"
            class="btn btn-ghost size-8 shrink-0 px-0"
            :disabled="room.archived"
            title="Attach a file"
            @click="filePicker?.click()"
          >
            <Paperclip class="size-4" />
          </button>

          <!-- The choice sits where the file is picked rather than in
               settings: it is a per-message decision, and someone sharing a
               screenshot whose text must stay sharp needs it to hand. -->
          <button
            type="button"
            class="btn size-8 shrink-0 px-0"
            :class="sendOriginal ? 'btn-primary' : 'btn-ghost'"
            :disabled="room.archived"
            :aria-pressed="sendOriginal"
            :title="
              sendOriginal
                ? 'Sending images at full size. Click to reduce them instead.'
                : 'Images are reduced before sending. Click to send them at full size.'
            "
            @click="sendOriginal = !sendOriginal"
          >
            <Maximize2 class="size-4" />
          </button>
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
            :disabled="
              sending || room.archived || (!draft.trim() && uploads.completed.value.length === 0)
            "
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

    <ContextMenu
      v-if="contextMenu.open.value"
      :items="contextMenu.items.value"
      :x="contextMenu.position.value.x"
      :y="contextMenu.position.value.y"
      @close="contextMenu.close"
    />

    <ConfirmDialog
      v-if="pendingDelete"
      title="Delete this message?"
      :detail="pendingDelete.body"
      confirm-label="Delete"
      @cancel="pendingDelete = null"
      @confirm="remove(pendingDelete.id)"
    />

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

<style scoped>
@reference "~/assets/css/main.css";

/*
 * Marks the message a search or a thread jumped to.
 *
 * :deep, because the class is set on a child component's root element rather
 * than on markup this file owns. It runs once and leaves nothing behind: it
 * says how the reader got here, not something about the message.
 */
:deep(.just-found) {
  animation: found 2s ease-out;
  border-radius: var(--radius-lg);
}

@keyframes found {
  0%,
  40% {
    background-color: color-mix(in oklab, var(--color-primary) 22%, transparent);
  }
  100% {
    background-color: transparent;
  }
}

/* Somebody who prefers less motion still needs to see which message it is, so
   the mark is shown and simply does not animate. */
@media (prefers-reduced-motion: reduce) {
  :deep(.just-found) {
    animation: none;
    background-color: color-mix(in oklab, var(--color-primary) 22%, transparent);
  }
}
</style>
