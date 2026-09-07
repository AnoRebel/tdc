<script setup lang="ts">
import type {
  Attachment,
  LiveEventPayload,
  Message,
  MessageLifetime,
  Presence,
  Room,
  TypingPerson,
} from "@tdc/types";
import { isApiError } from "@tdc/types";
import type { MenuItem } from "~/components/ContextMenu.vue";
import {
  Check,
  CheckSquare,
  ChevronDown,
  Clock,
  Copy,
  Eye,
  EyeOff,
  Timer,
  FileText,
  Flag,
  Forward,
  Hash,
  Image as ImageIcon,
  Lock,
  Maximize2,
  Menu,
  MessageSquareText,
  MoreVertical,
  Music,
  Paperclip,
  Pencil,
  Pin,
  SendHorizontal,
  ShieldOff,
  Smile,
  Trash2,
  UserX,
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

const { user, can } = useAuth();
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

const reactions = useReactions(roomId);
const pins = usePins(roomId);

/*
 * Messages the reader has picked out, for acting on several at once.
 *
 * A Set rather than a flag on each message: the timeline is rebuilt from the
 * event log on every change, so a flag stored on a message would be lost the
 * moment an edit or a deletion arrived.
 */
const selected = ref<Set<string>>(new Set());
const selecting = computed(() => selected.value.size > 0);

/** What is being forwarded, or reported, while the dialog is open. */
const forwarding = ref<string[]>([]);
const reporting = ref<{ id: string; body: string } | null>(null);
const expiring = ref<{ id: string; current?: MessageLifetime } | null>(null);

function toggleSelected(messageId: string) {
  const next = new Set(selected.value);
  if (next.has(messageId)) {
    next.delete(messageId);
  } else {
    next.add(messageId);
  }
  selected.value = next;
}

function clearSelection() {
  selected.value = new Set();
}

/*
 * Brief confirmations for the two actions that leave no trace.
 *
 * A scheduled message does not appear in the conversation and a report has no
 * visible effect, so without a word somebody would do either of them twice.
 *
 * Declared here, above everything that sets them: const is not hoisted, and
 * assigning to one of these from a handler defined earlier in the file threw a
 * reference error that silently swallowed the confirmation.
 */
const scheduled = ref(false);
const reported = ref(false);

watch([scheduled, reported], () => {
  if (scheduled.value) setTimeout(() => (scheduled.value = false), 4000);
  if (reported.value) setTimeout(() => (reported.value = false), 4000);
});

/*
 * The tab reads as the conversation, with unread messages counted in front.
 *
 * A count in the title is what makes a background tab useful: somebody working
 * in another window sees "(3) General" without switching to it, which is the
 * whole reason a browser puts the title where it does.
 *
 * The conversation's own unread count rather than every notification: this tab
 * is that conversation, and a number covering other channels would be a number
 * about something the reader is not looking at.
 */
useHead({
  title: computed(() => {
    const name = room.value?.name;
    if (!name) return "Loading";

    const waiting = unreadHere.value;
    return waiting > 0 ? `(${waiting}) ${name}` : name;
  }),
});

/** How many messages are unread in this conversation. */
const unreadHere = ref(0);

const api = useApi();

/*
 * The layout's drawer, on a narrow screen.
 *
 * Injected rather than passed: the drawer belongs to the layout, and the
 * button that opens it is in this page's own header. A fallback ref keeps this
 * page usable outside that layout — a test rendering it alone should not need
 * to provide one.
 */
const navigationOpen = inject<Ref<boolean>>("navigationOpen", ref(false));

/*
 * What the keyboard can do to a conversation.
 *
 * Registered with the layout, which owns the bindings: only this page knows
 * where the message box is or which message is last, and only the layout knows
 * that a key was pressed. A page that is not a conversation registers nothing
 * and the keys do nothing, which is the right outcome rather than an error.
 */
const composerFocus = inject<Ref<(() => void) | null>>("composerFocus", ref(null));
const threadOpen = inject<Ref<(() => void) | null>>("threadOpen", ref(null));

/*
 * Touch gestures over the conversation.
 *
 * Each is a shortcut for somebody who knows it and never the only way to reach
 * something: the channel list, the reply button and the refresh on reconnect
 * all still work. An interaction nobody can discover should not be the only
 * route to anything.
 */
const moveChannel = inject<(direction: number) => void>("moveChannel", () => {});

useConversationSwipes(timelineElement, {
  // Swiping a message opens a reply to it, which is what the reply action in
  // its own menu does.
  reply: (messageId) => openThread(messageId),
  previous: () => moveChannel(-1),
  next: () => moveChannel(1),
});

const pull = usePullToRefresh(timelineElement, () => load());

onMounted(() => {
  composerFocus.value = () => composer.value?.focus();

  // The most recent message with something to reply to. Opening a thread on
  // nothing would be a key that appears broken.
  threadOpen.value = () => {
    const last = [...visible.value].reverse().find((message) => !message.removed);
    if (last) openThread(last.id);
  };
});

// Cleared on leaving, so the keys do nothing rather than reaching into a
// conversation that is no longer on screen.
onUnmounted(() => {
  composerFocus.value = null;
  threadOpen.value = null;
});
const personal = usePersonalModeration();

/** Who is about to be blocked, while the confirmation is up. */
const blocking = ref<{ sender: string } | null>(null);

/*
 * Blocks somebody by their address.
 *
 * Their identifier is found through search rather than carried on the message,
 * which names people by address so a federated sender reads the same as a
 * local one.
 */
async function blockSender(sender: string) {
  const username = shortName(sender);
  blocking.value = null;

  try {
    const found = await api.get<{ people: { id: string; username: string }[] }>(
      `/api/search/everything?q=${encodeURIComponent(username)}&messages=false`,
    );

    const person = found.people.find(
      (candidate: { id: string; username: string }) => candidate.username === username,
    );
    if (!person) {
      error.value = "Could not find that person to block.";
      return;
    }

    await personal.block(person.id);
    blocked.value = true;
  } catch {
    error.value = "Could not block that person.";
  }
}

/*
 * Said once, briefly.
 *
 * Blocking has no visible effect in the conversation — that is the point — so
 * without a word nothing would confirm it happened.
 */
const blocked = ref(false);
watch(blocked, (done) => {
  if (done) setTimeout(() => (blocked.value = false), 4000);
});

const showPinned = ref(false);
const showBans = ref(false);

/** Who is about to be banned, while the form is up. */
const banning = ref<{ sender: string } | null>(null);

/*
 * Bans somebody from this channel.
 *
 * The reason is required by the server and asked for here rather than
 * defaulted: a ban nobody can explain is a ban nobody can review, and the
 * person banned is entitled to it.
 */
async function banSender(sender: string, reason: string, hours?: number) {
  const username = shortName(sender);
  banning.value = null;

  try {
    const found = await api.get<{ people: { id: string; username: string }[] }>(
      `/api/search/everything?q=${encodeURIComponent(username)}&messages=false`,
    );

    const person = found.people.find(
      (candidate: { id: string; username: string }) => candidate.username === username,
    );
    if (!person) {
      error.value = "Could not find that person to ban.";
      return;
    }

    await roomsApi.banFromChannel(roomId.value, person.id, reason, hours);

    // Shown at once, so the moderator sees the result of what they just did
    // rather than having to go looking for it.
    showBans.value = true;
  } catch (cause) {
    error.value = isApiError(cause) ? cause.message : "Could not ban that person.";
  }
}
const attaching = ref(false);
const choosingEmoji = ref(false);
const scheduling = ref(false);

/*
 * Only one composer menu at a time.
 *
 * Opening the emoji picker while the attachment menu was up left both on
 * screen, overlapping, with no way to tell which a click would reach. Each
 * closes the other rather than each carrying its own dismissal logic.
 */
watch(attaching, (open) => {
  if (open) choosingEmoji.value = false;
});
watch(choosingEmoji, (open) => {
  if (open) attaching.value = false;
});

/** Closes both when the reader clicks anywhere else. */
function closeComposerMenus(event: MouseEvent) {
  if (!attaching.value && !choosingEmoji.value) return;

  const target = event.target as HTMLElement;
  if (target.closest("[data-composer-menu]")) return;

  attaching.value = false;
  choosingEmoji.value = false;
}

onMounted(() => document.addEventListener("click", closeComposerMenus));
onBeforeUnmount(() => document.removeEventListener("click", closeComposerMenus));

/*
 * What the file picker should offer.
 *
 * A phone's chooser is a different screen for photos, for the camera and for
 * documents, and it decides which one to show from the accept attribute. A
 * bare "attach" opens the file browser, which is the least useful of the
 * three for the thing people most often send.
 */
const pickerAccept = ref("");

const attachmentOptions = [
  { label: "Photo or video", accept: "image/*,video/*", icon: ImageIcon },
  { label: "Audio", accept: "audio/*", icon: Music },
  {
    label: "Document",
    accept: ".pdf,.doc,.docx,.xls,.xlsx,.ppt,.pptx,.odt,.ods,.txt,.md",
    icon: FileText,
  },
  { label: "Any file", accept: "", icon: Paperclip },
];

/** Opens the picker filtered to one kind. */
async function pickWith(accept: string) {
  attaching.value = false;
  pickerAccept.value = accept;

  // The attribute has to reach the element before it is opened, or the picker
  // shows whatever the previous choice was.
  await nextTick();
  filePicker.value?.click();
}

/** Puts an emoji where the cursor is, rather than at the end. */
function insertEmoji(emoji: string) {
  const element = composer.value;
  if (!element) {
    draft.value += emoji;
    return;
  }

  const start = element.selectionStart ?? draft.value.length;
  const end = element.selectionEnd ?? start;

  draft.value = draft.value.slice(0, start) + emoji + draft.value.slice(end);

  // Back to just after what was inserted, so typing continues where the
  // reader was rather than at the end of the message.
  void nextTick(() => {
    element.focus();
    element.selectionStart = element.selectionEnd = start + emoji.length;
  });
}

/*
attachGif uploads a chosen GIF as an ordinary attachment.

Fetched here and uploaded rather than sent as a link, so the GIF behaves like
everything else: stored on this server, content-addressed and deduplicated,
readable to somebody with no route to the provider, and still there when the
provider takes it down or blocks the reader's country.

The fetch is the browser's rather than the server's. The URL comes from a
provider this server just called, so it is not arbitrary — but a server-side
fetch of a URL a third party supplied is a request worth not making.
*/
async function attachGif(url: string) {
  try {
    const response = await fetch(url);
    if (!response.ok) throw new Error(String(response.status));

    const blob = await response.blob();

    // Named from the URL where possible, since the filename is what a reader
    // sees if the image itself will not display.
    const name = url.split("/").pop()?.split("?")[0] || "animation.gif";
    const file = new File([blob], name.endsWith(".gif") ? name : `${name}.gif`, {
      type: blob.type || "image/gif",
    });

    await uploads.upload(roomId.value, file);
  } catch {
    error.value = "Could not attach that GIF.";
  }
}

/** Writes a caption on one uploaded file, before the message is sent. */
async function captionAttachment(attachmentId: string, caption: string) {
  try {
    await roomsApi.captionAttachment(attachmentId, caption);
  } catch {
    // A caption that does not save leaves the file without one, which is a
    // smaller loss than interrupting somebody mid-message to say so.
  }
}

/** Sends the message later rather than now. */
async function scheduleDraft(sendAt: Date) {
  const body = draft.value.trim();
  scheduling.value = false;
  if (!body) return;

  try {
    await roomsApi.schedule(
      roomId.value,
      body,
      sendAt,
      uploads.uploads.value
        .filter((upload) => upload.attachment)
        .map((upload) => upload.attachment!.id),
    );
    draft.value = "";
    uploads.clearFinished();
    scheduled.value = true;
  } catch (cause) {
    error.value = isApiError(cause) ? cause.message : "Could not schedule that message.";
  }
}

/** Every other room this person can post in, as forwarding destinations. */
const forwardableRooms = ref<Room[]>([]);

/** The readable text of a message, for the pinned list. */
function bodyOf(message: Message): string {
  const content = message.content as { body?: string } | undefined;
  return content?.body ?? "";
}

/**
 * Copies the selected messages as text.
 *
 * Attributed and in order, because a block of quoted lines with no names is
 * not something anybody can paste anywhere useful.
 */
async function copySelected() {
  const lines = visible.value
    .filter((message) => selected.value.has(message.id))
    .map((message) => `${shortName(message.sender)}: ${message.body}`);

  try {
    await navigator.clipboard?.writeText(lines.join("\n"));
  } catch {
    // Clipboard access can be refused. Nothing is lost that the reader cannot
    // do by selecting the text themselves.
  }
  clearSelection();
}

/**
 * Forwards the selected messages to another conversation.
 *
 * One request each rather than one for all: each becomes its own message at
 * the destination, which is what keeps a forwarded conversation readable in
 * order rather than collapsed into one wall of quoted text.
 */
async function forwardTo(destination: string) {
  const ids = [...forwarding.value];
  forwarding.value = [];

  try {
    for (const id of ids) {
      await roomsApi.forward(roomId.value, id, destination);
    }
    clearSelection();
  } catch (cause) {
    error.value = isApiError(cause) ? cause.message : "Could not forward that.";
  }
}

/** Files a report for a moderator to look at. */
async function submitReport(filing: { reason: string; detail: string }) {
  const message = reporting.value;
  reporting.value = null;
  if (!message) return;

  try {
    await roomsApi.report({
      room: roomId.value,
      eventId: message.id,
      reason: filing.reason,
      detail: filing.detail,
    });
    reported.value = true;
  } catch (cause) {
    error.value = isApiError(cause) ? cause.message : "Could not file that report.";
  }
}

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

    // Suppresses notifications for this conversation while it is on screen:
    // one about a message just watched arrive is pure interruption.
    live.viewing(roomId.value);
    void refreshThreadCounts();
    void reactions.refresh();
    void pins.refresh();
    void refreshUnreadHere();
    void loadForwardable();
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

/*
 * Reads this conversation's unread count, for the tab title.
 *
 * Read from the server rather than counted locally, because the count has to
 * survive a reload and agree with what every other client shows — a number the
 * tab invented would drift from the badge beside the channel name.
 */
async function refreshUnreadHere() {
  try {
    const counts = await roomsApi.unread();
    unreadHere.value = counts[roomId.value] ?? 0;
  } catch {
    // A title without a count is still a correct title.
  }
}

/** Rooms this person is in, other than the current one. */
async function loadForwardable() {
  try {
    const all = await roomsApi.list();
    forwardableRooms.value = all.filter(
      (candidate) => candidate.member && candidate.id !== roomId.value,
    );
  } catch {
    forwardableRooms.value = [];
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
  message: {
    id: string;
    body: string;
    removed: boolean;
    attachments?: Attachment[];
    lifetime?: MessageLifetime;
  },
  mine: boolean,
  sender: string,
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
    items.push({
      label: "Forward",
      icon: Forward,
      action: () => {
        forwarding.value = [message.id];
      },
    });
    items.push({
      label: pins.pinnedIds.value.has(message.id) ? "Unpin" : "Pin to channel",
      icon: Pin,
      action: () => togglePin(message.id),
    });
    items.push({
      label: "Select",
      icon: CheckSquare,
      action: () => toggleSelected(message.id),
    });
  }

  /*
   * When this message expires.
   *
   * Its sender's to set, or a moderator's on anybody's. Offered only while the
   * message is readable: putting a lifetime on something already gone would
   * do nothing, and offering it would suggest otherwise.
   */
  if ((mine || can("moderation.review")) && !message.removed) {
    items.push({
      label: message.lifetime ? "Change when it expires" : "Expire this message",
      icon: Timer,
      action: () => {
        expiring.value = { id: message.id, current: message.lifetime };
      },
    });
  }

  /*
   * Marking media sensitive, offered on any message carrying files that this
   * person may mark: their own, or anybody's for a moderator.
   *
   * One control for the whole message rather than one per file. A message is
   * usually four photographs from the same moment, and marking them
   * individually is four decisions where somebody meant to make one.
   */
  // Whether this person may mark is a fact about the message, not about each
  // file: they either sent it or they moderate.
  const markable = mine || can("moderation.review") ? (message.attachments ?? []) : [];

  if (markable.length && !message.removed) {
    // A moderator cannot lift their own mark from here without seeing which
    // it is, so the label follows what is currently true of the message.
    const anyCovered = markable.some((attachment) => attachment.sensitive);

    // Somebody who is not a moderator cannot lift a moderator's mark, so the
    // control is not offered when that is all there is to lift.
    const theirsToLift =
      can("moderation.review") ||
      markable.some((attachment) => attachment.sensitiveSource !== "moderator");

    if (!anyCovered || theirsToLift) {
      items.push({
        label: anyCovered ? "Unmark as sensitive" : "Mark as sensitive",
        icon: anyCovered ? Eye : EyeOff,
        action: () => setSensitive(markable, !anyCovered),
      });
    }
  }

  // Reporting somebody else's message, never one's own: there is nothing to
  // tell a moderator about a message you wrote and can delete yourself.
  if (!mine && !message.removed) {
    items.push({
      label: "Report",
      icon: Flag,
      action: () => {
        reporting.value = { id: message.id, body: message.body };
      },
    });

    // Blocking the person, which is a personal boundary rather than a report:
    // nobody is told, least of all them.
    items.push({
      label: `Block ${shortName(sender)}`,
      icon: UserX,
      destructive: true,
      action: () => {
        blocking.value = { sender };
      },
    });

    // Banning, which is a moderator's decision rather than a personal one, so
    // it appears only for whoever holds that authority.
    if (can("moderation.restrict")) {
      items.push({
        label: `Ban ${shortName(sender)} from this channel`,
        icon: ShieldOff,
        destructive: true,
        action: () => {
          banning.value = { sender };
        },
      });
    }
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

/*
 * Pins or unpins a message.
 *
 * Pinning speaks for the room rather than the person, so the server requires a
 * permission for it. A refusal is reported rather than swallowed: somebody who
 * clicked pin and saw nothing happen would click it again.
 */
/**
 * Marks or unmarks a message's files as sensitive.
 *
 * Each file is a separate request, since the server governs each one
 * individually — a moderator's mark on one of four does not decide the other
 * three. Failures are per file too: three succeeding and one being refused is
 * a real outcome, and treating it as total failure would hide the three.
 */
/**
 * Gives a message a lifetime, or shortens the one it has.
 *
 * The server refuses anything longer, and says so — which is reported rather
 * than swallowed, because somebody who tried to extend one needs to know it
 * did not happen.
 */
/**
 * How long a message has left, in words.
 *
 * A seen-based lifetime has no expiry until people have read it, so it says
 * what it is waiting for rather than counting down from nothing.
 */
function lifetimeLabel(lifetime: MessageLifetime): string {
  if (!lifetime.expiresAt) {
    return lifetime.kind === "once_seen" ? "Goes when read" : "Goes after reading";
  }

  const remaining = new Date(lifetime.expiresAt).getTime() - Date.now();
  if (remaining <= 0) return "Going";

  const minutes = Math.round(remaining / 60000);
  if (minutes < 60) return `${minutes}m left`;

  const hours = Math.round(minutes / 60);
  if (hours < 48) return `${hours}h left`;

  return `${Math.round(hours / 24)}d left`;
}

/** The whole rule, for somebody who wants to know exactly what happens. */
function lifetimeExplanation(lifetime: MessageLifetime): string {
  switch (lifetime.kind) {
    case "once_seen":
      return "This message goes once everybody has read it.";
    case "after_seeing":
      return "This message goes a set time after each person first reads it.";
    default:
      return "This message goes a set time after it was sent.";
  }
}

async function setLifetime(messageId: string, lifetime: { kind: string; seconds?: number }) {
  expiring.value = null;
  try {
    await api.put(
      `/api/rooms/${roomId.value}/messages/${encodeURIComponent(messageId)}/lifetime`,
      lifetime,
    );
    await load();
  } catch (cause) {
    error.value = isApiError(cause) ? cause.message : "That expiry was not accepted.";
  }
}

async function setSensitive(files: { id: string }[], sensitive: boolean) {
  await Promise.allSettled(
    files.map((file) => api.put(`/api/attachments/${file.id}/sensitive`, { sensitive })),
  );
  await load();
}

/*
 * How long messages here last, in words rather than seconds.
 *
 * Rounded to the unit somebody thinks in: "7 days" rather than "604800
 * seconds". Empty when nothing expires, so the header says nothing rather than
 * saying "kept forever" — the absence of a warning is the ordinary case and
 * should not need reading.
 */
const retentionLabel = computed(() => {
  const seconds = room.value?.retentionSeconds ?? 0;
  if (seconds <= 0) return "";

  const units: [number, string][] = [
    [86400, "day"],
    [3600, "hour"],
    [60, "minute"],
  ];

  for (const [size, name] of units) {
    if (seconds >= size) {
      const count = Math.round(seconds / size);
      return `Kept ${count} ${name}${count === 1 ? "" : "s"}`;
    }
  }
  return `Kept ${seconds} seconds`;
});

async function togglePin(messageId: string) {
  try {
    if (pins.pinnedIds.value.has(messageId)) {
      await pins.unpin(messageId);
    } else {
      await pins.pin(messageId);
    }
  } catch (cause) {
    error.value = isApiError(cause)
      ? cause.requiredPermission
        ? "You do not have permission to pin messages in this channel."
        : cause.message
      : "Could not pin that message.";
  }
}

/*
 * Stops claiming to be watching when the window is not in front.
 *
 * A tab left open on a conversation is not somebody reading it. Without this,
 * a laptop open on one channel would silently swallow its notifications for as
 * long as it stayed open.
 */
function onVisibilityChange() {
  live.viewing(document.hidden ? null : roomId.value);
}

onMounted(() => {
  document.addEventListener("visibilitychange", onVisibilityChange);
  window.addEventListener("blur", () => live.viewing(null));
  window.addEventListener("focus", onVisibilityChange);
});

onBeforeUnmount(() => {
  document.removeEventListener("visibilitychange", onVisibilityChange);
  // Leaving the page is no longer watching anything.
  live.viewing(null);
});

/** Adds or removes one of the reader's own reactions. */
function react(messageId: string, emoji: string) {
  void reactions.toggle(messageId, emoji, user.value?.address ?? "");
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
  try {
    const summaries = await roomsApi.threadSummaries(roomId.value);

    const counts: Record<string, number> = {};
    for (const [rootId, summary] of Object.entries(summaries)) {
      if (summary.replyCount > 0) counts[rootId] = summary.replyCount;
    }
    threadCounts.value = counts;
  } catch {
    // Counts are decoration on a timeline that has already loaded. Losing them
    // is not worth an error over what is otherwise a working conversation.
  }
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
      <header class="flex h-12 shrink-0 items-center gap-3 border-b px-3 md:px-4">
        <!-- Only where the sidebar is a drawer. On a wide screen the channel
             list is already visible and a button to reveal it would do
             nothing. -->
        <button
          type="button"
          class="btn btn-ghost size-8 shrink-0 px-0 md:hidden"
          aria-label="Channels"
          @click="navigationOpen = true"
        >
          <Menu class="size-4" />
        </button>

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
          <!-- The retention policy, said where somebody about to type can see
               it. A conversation that quietly deletes itself is one people say
               things in they would not have said knowing. -->
          <span
            v-if="retentionLabel"
            class="flex items-center gap-1.5 text-muted-foreground"
            :title="
              room?.retentionFromChannel
                ? 'This channel keeps messages for a set time.'
                : 'This server keeps messages for a set time.'
            "
          >
            <Clock class="size-3.5" />
            <span>{{ retentionLabel }}</span>
          </span>

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

          <!-- Only for whoever moderates: a ban that can only be issued is a
               ban nobody reverses, so the way back in lives beside it. -->
          <button
            v-if="can('moderation.restrict')"
            type="button"
            class="btn btn-ghost size-7 px-0"
            :aria-label="showBans ? 'Hide bans' : 'Show who is banned'"
            :title="showBans ? 'Hide bans' : 'Show who is banned'"
            @click="showBans = !showBans"
          >
            <ShieldOff class="size-3.5" />
          </button>
        </div>
      </header>

      <div v-if="showBans" class="shrink-0 border-b bg-muted/10 px-4 py-3">
        <ChannelBans :room-id="roomId" />
      </div>

      <!-- What the channel has marked as important, collapsed to one line: a
           list of pinned messages above every conversation would push the
           conversation itself off the screen. -->
      <button
        v-if="pins.pinned.value.length"
        type="button"
        class="flex w-full shrink-0 cursor-pointer items-center gap-2 border-b bg-muted/30 px-4 py-1.5 text-left text-xs transition-colors hover:bg-muted/50"
        @click="showPinned = !showPinned"
      >
        <Pin class="size-3.5 shrink-0 text-muted-foreground" />
        <span class="truncate text-muted-foreground">
          {{
            pins.pinned.value.length === 1
              ? "1 pinned message"
              : `${pins.pinned.value.length} pinned messages`
          }}
        </span>
        <ChevronDown
          class="ml-auto size-3.5 shrink-0 text-muted-foreground transition-transform"
          :class="showPinned && 'rotate-180'"
        />
      </button>

      <div v-if="showPinned && pins.pinned.value.length" class="shrink-0 border-b bg-muted/10">
        <ul class="max-h-40 overflow-y-auto p-2">
          <li v-for="item in pins.pinned.value" :key="item.message.id">
            <button
              type="button"
              class="flex w-full cursor-pointer items-start gap-2 rounded-lg px-2 py-1.5 text-left transition-colors hover:bg-accent"
              @click="jumpTo(item.message.id)"
            >
              <Avatar :name="item.message.sender" size="sm" class="mt-0.5 shrink-0" />
              <span class="min-w-0 flex-1">
                <span class="block truncate text-xs font-medium">
                  {{ shortName(item.message.sender) }}
                </span>
                <span class="block truncate text-xs text-muted-foreground">
                  {{ bodyOf(item.message) }}
                </span>
              </span>
            </button>
          </li>
        </ul>
      </div>

      <!-- Acting on several messages at once. Appears only while something is
           selected, so it costs nothing the rest of the time. -->
      <div
        v-if="selecting"
        class="flex shrink-0 items-center gap-2 border-b bg-accent/40 px-4 py-2 text-xs"
      >
        <span class="font-medium">
          {{ selected.size === 1 ? "1 message selected" : `${selected.size} messages selected` }}
        </span>
        <button type="button" class="btn btn-ghost h-7 px-2" @click="forwarding = [...selected]">
          <Forward class="size-3.5" />
          Forward
        </button>
        <button type="button" class="btn btn-ghost h-7 px-2" @click="copySelected">
          <Copy class="size-3.5" />
          Copy
        </button>
        <button type="button" class="btn btn-ghost ml-auto h-7 px-2" @click="clearSelection">
          Cancel
        </button>
      </div>

      <p
        v-if="error"
        class="shrink-0 border-b bg-destructive/10 px-4 py-2 text-sm text-destructive"
        role="alert"
      >
        {{ error }}
      </p>

      <!-- What a pull is doing, so the gesture is not silent. A pull with no
           feedback reads as the page failing to scroll. -->
      <div
        v-if="pull.distance.value > 0 || pull.refreshing.value"
        class="flex shrink-0 items-center justify-center gap-2 overflow-hidden text-xs text-muted-foreground transition-[height]"
        :style="{ height: `${pull.refreshing.value ? 32 : Math.min(pull.distance.value, 48)}px` }"
        aria-live="polite"
      >
        <Spinner v-if="pull.refreshing.value" class="size-3.5" />
        <span>{{ pull.refreshing.value ? "Refreshing" : "Pull to refresh" }}</span>
      </div>

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
                :data-message-id="message.id"
                v-bind="contextMenu.handlers(() => menuFor(message, group.mine, group.sender))"
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
                        menuFor(message, group.mine, group.sender),
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

                <!-- Only while selecting: a checkbox beside every message all
                     the time would add a column of controls to a conversation
                     that is mostly read rather than acted on. -->
                <label
                  v-if="selecting && !message.removed"
                  class="flex shrink-0 cursor-pointer items-center"
                  :class="group.mine ? 'order-last' : 'order-first'"
                >
                  <input
                    type="checkbox"
                    class="size-4 cursor-pointer accent-primary"
                    :checked="selected.has(message.id)"
                    :aria-label="`Select this message`"
                    @change="toggleSelected(message.id)"
                  />
                </label>

                <div
                  class="flex w-fit max-w-full flex-col gap-1"
                  :class="group.mine ? 'items-end' : 'items-start'"
                >
                  <span
                    v-if="pins.pinnedIds.value.has(message.id)"
                    class="flex items-center gap-1 text-[0.6875rem] text-muted-foreground"
                  >
                    <Pin class="size-3" />
                    Pinned
                  </span>
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
                      <template v-if="message.removed">This message was removed</template>
                      <MessageBody v-else :body="message.body" />
                      <span
                        v-if="message.edited && !message.removed"
                        class="ml-1 align-baseline text-[0.6875rem] opacity-60"
                      >
                        edited
                      </span>
                      <!-- Said on the message itself rather than on hover:
                           somebody deciding whether to answer needs to know it
                           is on a timer without going looking. -->
                      <span
                        v-if="message.lifetime && !message.removed"
                        class="ml-1 inline-flex items-center gap-0.5 align-baseline text-[0.6875rem] opacity-70"
                        :title="lifetimeExplanation(message.lifetime)"
                      >
                        <Timer class="size-2.5" />
                        {{ lifetimeLabel(message.lifetime) }}
                      </span>
                    </template>
                  </Bubble>

                  <AttachmentList
                    v-if="!message.removed && message.attachments.length"
                    :attachments="message.attachments"
                  />

                  <ReactionBar
                    v-if="!message.removed"
                    :reactions="reactions.forEvent(message.id)"
                    :can-react="!!room?.member"
                    @react="react(message.id, $event)"
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
          @caption="captionAttachment"
        />

        <form
          v-if="room?.member"
          class="flex items-end gap-2 rounded-xl border bg-surface p-1.5 shadow-xs transition focus-within:border-ring focus-within:shadow-sm"
          @submit.prevent="send"
        >
          <input
            ref="filePicker"
            type="file"
            multiple
            :accept="pickerAccept"
            class="sr-only"
            @change="pickFiles"
          />

          <!-- A menu rather than one button, so the picker can be told what to
               offer. A phone's file chooser is a different screen for photos,
               for the camera and for documents, and a bare "attach" opens the
               least useful of the three. -->
          <div class="relative shrink-0" data-composer-menu>
            <button
              type="button"
              class="btn btn-ghost size-8 px-0"
              :disabled="room.archived"
              title="Attach"
              aria-label="Attach"
              @click="attaching = !attaching"
            >
              <Paperclip class="size-4" />
            </button>

            <div
              v-if="attaching"
              class="absolute bottom-full left-0 z-20 mb-1 w-44 overflow-hidden rounded-lg border border-border bg-card py-1 shadow-lg"
            >
              <button
                v-for="option in attachmentOptions"
                :key="option.label"
                type="button"
                class="flex w-full cursor-pointer items-center gap-2 px-3 py-1.5 text-left text-sm transition-colors hover:bg-accent"
                @click="pickWith(option.accept)"
              >
                <component :is="option.icon" class="size-4 shrink-0 text-muted-foreground" />
                {{ option.label }}
              </button>
            </div>
          </div>

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
          <button
            type="button"
            class="btn btn-ghost size-8 shrink-0 px-0"
            :disabled="room.archived || !draft.trim()"
            title="Send later"
            aria-label="Send later"
            @click="scheduling = true"
          >
            <Clock class="size-4" />
          </button>

          <div class="relative shrink-0" data-composer-menu>
            <button
              type="button"
              class="btn btn-ghost size-8 px-0"
              :disabled="room.archived"
              title="Emoji"
              aria-label="Emoji"
              @click="choosingEmoji = !choosingEmoji"
            >
              <Smile class="size-4" />
            </button>

            <div v-if="choosingEmoji" class="absolute bottom-full left-0 z-20 mb-1">
              <EmojiPicker v-model="choosingEmoji" @pick="insertEmoji" @gif="attachGif" />
            </div>
          </div>

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

    <ConfirmDialog
      v-if="blocking"
      title="Block this person?"
      :detail="`${shortName(blocking.sender)} will not be able to start a conversation with you. They are not told.`"
      confirm-label="Block"
      @cancel="blocking = null"
      @confirm="blockSender(blocking.sender)"
    />

    <p
      v-if="blocked"
      class="fixed bottom-6 left-1/2 z-40 -translate-x-1/2 rounded-full bg-foreground px-4 py-2 text-xs font-medium text-background shadow-lg"
      role="status"
    >
      Blocked. They are not told.
    </p>

    <BanDialog
      v-if="banning"
      :username="shortName(banning.sender)"
      @cancel="banning = null"
      @ban="(reason, hours) => banSender(banning!.sender, reason, hours)"
    />

    <ScheduleDialog
      v-if="scheduling"
      :body="draft"
      @cancel="scheduling = false"
      @schedule="scheduleDraft"
    />

    <!-- Both of these leave no trace in the conversation, so the interface has
         to say the thing happened or somebody will do it again. -->
    <p
      v-if="scheduled"
      class="fixed bottom-6 left-1/2 z-40 -translate-x-1/2 rounded-full bg-foreground px-4 py-2 text-xs font-medium text-background shadow-lg"
      role="status"
    >
      Scheduled. It will send at the time you chose.
    </p>

    <p
      v-if="reported"
      class="fixed bottom-6 left-1/2 z-40 -translate-x-1/2 rounded-full bg-foreground px-4 py-2 text-xs font-medium text-background shadow-lg"
      role="status"
    >
      Reported. A moderator will look at it.
    </p>

    <ForwardDialog
      v-if="forwarding.length"
      :count="forwarding.length"
      :rooms="forwardableRooms"
      @cancel="forwarding = []"
      @forward="forwardTo"
    />

    <ReportDialog
      v-if="reporting"
      :body="reporting.body"
      @cancel="reporting = null"
      @report="submitReport"
    />

    <LifetimeDialog
      v-if="expiring"
      :current="expiring.current"
      @cancel="expiring = null"
      @choose="(lifetime) => setLifetime(expiring!.id, lifetime)"
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
