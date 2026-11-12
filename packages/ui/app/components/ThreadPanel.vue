<script setup lang="ts">
import { useDropZone, useIntervalFn } from "@vueuse/core";
import type { Message, Room, TypingPerson } from "@tdc/types";
import { isApiError } from "@tdc/types";
import { Bell, BellOff, Paperclip, X } from "lucide-vue-next";
import { Button } from "#layers/ui/app/components/ui/button";
import { Separator } from "#layers/ui/app/components/ui/separator";
import { Skeleton } from "#layers/ui/app/components/ui/skeleton";
import { isOwnAddress } from "#layers/ui/app/lib/names";
import { foldMessages, groupMessages } from "#layers/ui/app/lib/timeline";

/**
 * A thread, opened beside the channel.
 *
 * A reply is a message, and this shows it as one: the same rows the channel
 * draws, the same composer, the same menu. Threads inherit the channel's
 * permissions, so a person who can see this panel can already read the
 * channel; what they may do to each message is the conversation's to say,
 * through the context it provides.
 *
 * Live events reach this through the conversation, which owns the one live
 * connection: a second connection per panel would be a second socket, and on
 * the desktop the two would close each other.
 */
const props = defineProps<{
  roomId: string;
  rootId: string;
  room: Room | null;
  /** A reply to bring into view and mark, when the thread was opened to show it. */
  highlight?: string | null;
}>();

const emit = defineEmits<{ close: []; replied: []; read: [] }>();

const { $t, $ts } = useI18n();
const { user } = useAuth();
const roomsApi = useRooms();
const context = useMessageContext();

const roomIdRef = computed(() => props.roomId);
const rootIdRef = computed<string | undefined>(() => props.rootId);
const reactions = useReactions(roomIdRef, rootIdRef);

const events = ref<Message[]>([]);
const loading = ref(true);
const error = ref<string | null>(null);
const following = ref(false);
/** How far the reader had read when the thread opened, for the "new" mark. */
const readAtOpen = ref(0);
const typingNow = ref<TypingPerson[]>([]);

const panel = ref<HTMLElement | null>(null);
const scroller = ref<HTMLElement | null>(null);
const composer = ref<{
  focus: () => void;
  addFiles: (files: File[]) => void;
} | null>(null);

const member = computed(() => context.member.value);
const mine = (sender: string) => isOwnAddress(user.value, sender);

const shown = computed(() => foldMessages(events.value));
const root = computed(() => shown.value.find((message) => message.id === props.rootId));
const replies = computed(() => shown.value.filter((message) => message.id !== props.rootId));

/*
 * Replies split where the unread ones begin, so a "new" rule can sit between
 * them. Fixed at what was unread on opening: marking the thread read must not
 * pull the rule out from under somebody who has not got to it yet.
 */
const firstNew = computed(() =>
  replies.value.findIndex((reply) => reply.sequence > readAtOpen.value && !mine(reply.sender)),
);
const earlierRuns = computed(() =>
  groupMessages(firstNew.value < 0 ? replies.value : replies.value.slice(0, firstNew.value), mine),
);
const newerRuns = computed(() =>
  firstNew.value < 0 ? [] : groupMessages(replies.value.slice(firstNew.value), mine),
);
const rootRun = computed(() => (root.value ? groupMessages([root.value], mine)[0] : undefined));

const replyCount = computed(() => replies.value.length);

const typingLabel = computed(() => {
  const names = typingNow.value.map((person) => person.displayName);
  if (names.length === 0) return "";
  if (names.length === 1) return $ts("room.typing", { names: names[0]! });
  if (names.length === 2) return $ts("room.typingTwo", { first: names[0]!, second: names[1]! });
  return $ts("room.typingCount", { count: names.length });
});

function scrollToEnd() {
  void nextTick(() => {
    const element = scroller.value;
    if (element) element.scrollTop = element.scrollHeight;
  });
}

/** Brings one message into view and marks it, the way the channel does. */
function bringIntoView(messageId: string) {
  void nextTick(() => {
    const element = panel.value?.querySelector<HTMLElement>(
      `[id="thread-message-${CSS.escape(messageId)}"]`,
    );
    if (!element) return scrollToEnd();
    element.scrollIntoView({ block: "center" });
    element.classList.add("just-found");
    setTimeout(() => element.classList.remove("just-found"), 2000);
  });
}

async function loadTyping() {
  try {
    const all = await roomsApi.typing(props.roomId);
    typingNow.value = all.filter((person) => person.threadRoot === props.rootId);
  } catch {
    // Decoration; the thread is readable without it.
  }
}

/**
 * Records that everything shown has been read, so the count beside the
 * message in the channel stops saying there is something new.
 */
function markRead() {
  if (!member.value || document.hidden) return;
  const latest = Math.max(0, ...events.value.map((event) => event.sequence));
  if (!latest) return;
  void roomsApi
    .markThreadRead(props.roomId, props.rootId, latest)
    .then(() => emit("read"))
    .catch(() => {
      // Unread state is a convenience, not worth interrupting anybody over.
    });
}

async function load() {
  loading.value = true;
  error.value = null;
  try {
    const thread = await roomsApi.thread(props.roomId, props.rootId);
    events.value = [thread.root, ...thread.replies];
    following.value = thread.following;
    readAtOpen.value = thread.readSequence;
    void reactions.refresh();
    void loadTyping();
    markRead();
  } catch (cause) {
    error.value = isApiError(cause) ? cause.message : $ts("room.couldNotOpenThread");
  } finally {
    loading.value = false;
    if (props.highlight) bringIntoView(props.highlight);
    else scrollToEnd();
  }
}

watch(() => props.rootId, load, { immediate: true });
watch(
  () => props.highlight,
  (id) => id && !loading.value && bringIntoView(id),
);

/** Whether an event belongs here: a reply, or something done to this thread's root. */
function concernsThisThread(event: Message): boolean {
  if (event.threadRoot === props.rootId || event.id === props.rootId) return true;
  const content = event.content as { replaces?: string; redacts?: string; updates?: string };
  return [content.replaces, content.redacts, content.updates].includes(props.rootId);
}

/**
 * An event from the conversation's live connection, or from something this
 * reader just did. Taken once: the sender receives their own event twice.
 */
function receive(event: Message) {
  if (!concernsThisThread(event)) return;
  if (events.value.some((existing) => existing.id === event.id)) return;

  const element = scroller.value;
  const atEnd = !element || element.scrollHeight - element.scrollTop - element.clientHeight < 80;

  events.value = [...events.value, event];
  if (event.type === "message.sent") {
    if (atEnd || mine(event.sender)) scrollToEnd();
    markRead();
    void loadTyping();
  }
}

defineExpose({ receive, reload: load });

async function sendReply(message: { body: string; attachmentIds: string[]; clientToken: string }) {
  const sent = await roomsApi.reply(
    props.roomId,
    props.rootId,
    message.body,
    message.clientToken,
    message.attachmentIds,
  );
  receive(sent);
  scrollToEnd();
  emit("replied");
}

/** The up arrow in an empty box: the reader's own latest reply, opened for editing. */
function editLast(): boolean {
  const last = [...replies.value].reverse().find((reply) => !reply.removed && mine(reply.sender));
  if (!last) return false;
  context.startEditing(last);
  bringIntoView(last.id);
  void nextTick(() =>
    panel.value
      ?.querySelector<HTMLTextAreaElement>(`[id="thread-message-${CSS.escape(last.id)}"] textarea`)
      ?.focus(),
  );
  return true;
}

async function toggleFollowing() {
  const next = !following.value;
  following.value = next;
  try {
    await roomsApi.followThread(props.roomId, props.rootId, next);
  } catch (cause) {
    following.value = !next;
    error.value = isApiError(cause) ? cause.message : $ts("room.couldNotFollow");
  }
}

/*
 * Files dropped anywhere on the panel go into the reply, as they would into
 * the channel's box. Only files: dragging text in is text being moved.
 */
const { isOverDropZone } = useDropZone(panel, {
  checkValidity: (items) => [...items].some((item) => item.kind === "file"),
  onDrop: (files) => {
    if (files?.length && member.value) composer.value?.addFiles(files);
  },
});

onMounted(() => {
  // Somebody who opened a thread is about to read or answer it.
  if (member.value) void nextTick(() => composer.value?.focus());
});

useEventListener(document, "visibilitychange", markRead);

// Typing has no live frame; asked again while the thread is on screen, as the
// channel does.
useIntervalFn(() => {
  if (member.value && document.visibilityState === "visible") void loadTyping();
}, 3000);
</script>

<template>
  <aside
    ref="panel"
    class="relative flex w-full shrink-0 flex-col border-l bg-surface-sunken max-md:fixed max-md:inset-0 max-md:z-30 md:w-96"
    :aria-label="$ts('room.thread')"
  >
    <header class="flex h-12 shrink-0 items-center gap-2 border-b px-3">
      <h2 class="text-sm font-semibold">{{ $t("room.thread") }}</h2>
      <span v-if="!loading" class="text-xs text-muted-foreground tabular">
        {{ replyCount === 1 ? $t("room.oneReply") : $t("room.manyReplies", { count: replyCount }) }}
      </span>
      <Button
        v-if="member && !loading"
        variant="ghost"
        size="icon"
        class="ml-auto"
        :aria-pressed="following"
        :label="following ? $ts('room.stopFollowingThread') : $ts('room.followThread')"
        :title="following ? $ts('room.followingThreadHint') : $ts('room.followThreadHint')"
        @click="toggleFollowing"
      >
        <Bell v-if="following" class="size-4" />
        <BellOff v-else class="size-4" />
      </Button>
      <Button
        variant="ghost"
        size="icon"
        :class="member && !loading ? '' : 'ml-auto'"
        :label="$ts('room.closeThread')"
        @click="emit('close')"
      >
        <X class="size-4" />
      </Button>
    </header>

    <p
      v-if="error"
      class="shrink-0 bg-destructive/10 px-3 py-2 text-sm text-destructive"
      role="alert"
    >
      {{ error }}
    </p>

    <div ref="scroller" class="flex-1 overflow-y-auto px-3 py-3">
      <div v-if="loading" class="space-y-4">
        <Skeleton class="h-16 rounded-lg" />
        <Skeleton class="h-10 w-3/4" />
      </div>

      <div v-else class="space-y-3">
        <!-- The message the thread hangs off, set apart so it reads as the
             subject rather than the first reply. -->
        <div v-if="rootRun" class="rounded-lg border bg-surface p-2.5">
          <TimelineGroup :run="rootRun" in-thread :reactions="reactions" id-prefix="thread-" />
        </div>

        <Separator
          v-if="replyCount"
          :label="
            replyCount === 1 ? $ts('room.oneReply') : $ts('room.manyReplies', { count: replyCount })
          "
        />
        <p v-else class="py-6 text-center text-xs text-muted-foreground">
          {{ $t("room.noRepliesYet") }}
        </p>

        <TimelineGroup
          v-for="(run, index) in earlierRuns"
          :key="`earlier-${index}-${run.items[0]?.id}`"
          :run="run"
          in-thread
          :reactions="reactions"
          id-prefix="thread-"
        />

        <!-- Where the unread replies begin. Said in words as well as drawn,
             so it is not colour alone that marks them. -->
        <div
          v-if="newerRuns.length"
          class="flex items-center gap-2 text-[0.6875rem] font-semibold text-primary"
          role="separator"
          :aria-label="$ts('room.newRepliesDivider')"
        >
          <span class="h-px flex-1 bg-primary/40" />
          {{ $t("room.newRepliesDivider") }}
          <span class="h-px flex-1 bg-primary/40" />
        </div>

        <TimelineGroup
          v-for="(run, index) in newerRuns"
          :key="`newer-${index}-${run.items[0]?.id}`"
          :run="run"
          in-thread
          :reactions="reactions"
          id-prefix="thread-"
        />
      </div>
    </div>

    <footer class="shrink-0 px-3 pt-1 pb-4">
      <p class="flex h-4 items-center gap-1.5 text-xs text-muted-foreground" aria-live="polite">
        <Transition name="motion-fade">
          <span v-if="typingLabel" class="motion-typing" aria-hidden="true">
            <span /><span /><span />
          </span>
        </Transition>
        {{ typingLabel }}
      </p>

      <MessageComposer
        v-if="member"
        ref="composer"
        :room-id="roomId"
        :thread-root="rootId"
        :draft-key="`${roomId}/thread/${rootId}`"
        :placeholder="$ts('room.replyPlaceholder')"
        :disabled="Boolean(room?.archived)"
        :send="sendReply"
        :edit-last="editLast"
        @error="error = $event"
      />
      <p v-else class="rounded-xl border bg-surface px-3 py-2.5 text-sm text-muted-foreground">
        {{ $t("room.joinToAnswer") }}
      </p>
    </footer>

    <!-- Where a dragged file will go, so letting go is not a guess. -->
    <div
      v-if="isOverDropZone && member"
      class="pointer-events-none absolute inset-2 z-40 grid place-items-center rounded-xl border-2 border-dashed border-primary bg-background/85 text-sm font-medium text-primary"
    >
      <span class="flex items-center gap-2">
        <Paperclip class="size-4" aria-hidden="true" />
        {{ $t("room.dropToAttachReply") }}
      </span>
    </div>
  </aside>
</template>
