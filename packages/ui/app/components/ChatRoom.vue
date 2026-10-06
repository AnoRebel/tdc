<script setup lang="ts">
import type {
  Attachment,
  Call,
  LiveEventPayload,
  Message,
  MessageAction,
  MessageLifetime,
  Presence,
  PrivateReply,
  ControlsUpdate,
  MessageVia,
  Room,
  TypingPerson,
} from "@tdc/types";
import { isApiError } from "@tdc/types";
import { readFailure } from "#layers/ui/app/lib/reachability";
import type { MenuItem } from "#layers/ui/app/components/ContextMenu.vue";
import {
  Check,
  CheckSquare,
  ChevronDown,
  Clock,
  CloudOff,
  Copy,
  Eye,
  EyeOff,
  Timer,
  FileText,
  Flag,
  Forward,
  Globe,
  Hash,
  Image as ImageIcon,
  Lock,
  Maximize2,
  Menu,
  MessageSquareText,
  MoreVertical,
  Music,
  Paperclip,
  Phone,
  Pencil,
  Pin,
  SendHorizontal,
  ShieldOff,
  Smile,
  Trash2,
  X,
  UserX,
} from "lucide-vue-next";
import { Avatar } from "#layers/ui/app/components/ui/avatar";
import { Bubble } from "#layers/ui/app/components/ui/bubble";
import { Kbd } from "#layers/ui/app/components/ui/kbd";
import { Message as MessageRow, MessageGroup } from "#layers/ui/app/components/ui/message";
import { Separator } from "#layers/ui/app/components/ui/separator";
import { Skeleton } from "#layers/ui/app/components/ui/skeleton";
import { Spinner } from "#layers/ui/app/components/ui/spinner";
import { Button } from "#layers/ui/app/components/ui/button";
import { Input } from "#layers/ui/app/components/ui/input";
import { atTime, onDay } from "#layers/ui/app/lib/dates";
import { isTyping } from "#layers/ui/app/lib/shortcuts";
import IssueFromMessage from "#layers/ui/app/components/projects/IssueFromMessage.vue";
import IssueLinks from "#layers/ui/app/components/projects/IssueLinks.vue";

const { $t, $ts } = useI18n();

/*
 * The conversation this shows, given by whichever page renders it.
 *
 * A prop rather than the route, because this is the shared body of a
 * conversation and each client wraps it in its own thin page — the web reads
 * the room from its route, the desktop from its own. Taking the route here
 * would tie the shared component to one client's routing.
 */
const props = defineProps<{ roomId: string }>();
const roomId = computed(() => props.roomId);

/*
 * The route, for the query alone — which message to open at, and the counter
 * that makes the same jump distinct. The room itself is the prop above, given
 * by whichever page wraps this; the query is read here because both clients
 * carry it the same way and a search result names its message in it.
 */
const route = useRoute();

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
const fetchMedia = useMediaFetch();
const outbox = useOutbox();

/** What this conversation is still holding, oldest first. */
const waiting = computed(() => outbox.waitingIn(roomId.value));

const lastSeen = useLastSeen<Message, Room>();
const drafts = useDrafts();

/*
 * What was being written here, restored.
 *
 * Watched rather than saved on every keystroke by hand: the draft belongs to
 * the conversation, and switching between two half-written messages should
 * find each where it was left.
 */
watch(
  roomId,
  (now, before) => {
    if (before && before !== now) drafts.keep(before, draft.value);
    draft.value = drafts.of(now);
    void nextTick().then(fitComposer);
  },
  { immediate: true },
);

watch(draft, (text) => drafts.keep(roomId.value, text));

/*
 * When what is on screen was last confirmed, or null if it is current.
 *
 * Drives the notice above the timeline. Somebody has to be able to tell a
 * conversation they are watching from one they are remembering.
 */
const showingLastSeen = ref<number | null>(null);

/*
 * Withdraws a message that has not gone yet.
 *
 * The text goes back into the composer rather than vanishing: somebody
 * withdrawing a message usually wants to change it, and making them retype it
 * would be a strange reward for noticing a mistake in time.
 */
function unsendWaiting(clientToken: string) {
  const message = waiting.value.find((waiting) => waiting.clientToken === clientToken);
  if (!message) return;

  outbox.unsend(clientToken);

  // Only into an empty composer: overwriting something half-typed would lose
  // the newer thought to recover the older one.
  if (!draft.value.trim()) {
    draft.value = message.body;
    void nextTick().then(fitComposer);
  }
}

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
// Which messages here became issues, and turning another into one.
const issueLinks = useIssueLinks(roomId);

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

/*
 * What the keyboard can do to a conversation.
 *
 * Registered with the layout, which owns the bindings: only this page knows
 * where the message box is or which message is last, and only the layout knows
 * that a key was pressed. Each registration lasts while this is mounted, so on
 * any other screen the keys do nothing rather than reach into a conversation
 * that is no longer there.
 */

/** The newest message still standing, optionally only the reader's own. */
function lastMessage(onlyMine = false) {
  return [...visible.value]
    .reverse()
    .find((message) => !message.removed && (!onlyMine || message.sender === user.value?.address));
}

/** Opens the reader's own latest message for editing, if there is one. */
function editLast(): boolean {
  const mine = lastMessage(true);
  if (!mine || !room.value?.member) return false;
  startEditing(mine);
  jumpTo(mine.id);

  // Into the field, so the keyboard that asked for the edit can make it: an
  // edit box opened from a key and left unfocused is one more Tab hunt.
  void nextTick(() =>
    document
      .getElementById(`message-${mine.id}`)
      ?.querySelector<HTMLInputElement | HTMLTextAreaElement>("input, textarea")
      ?.focus(),
  );
  return true;
}

useShortcutAction("composer", () => {
  if (!composer.value) return false;
  composer.value.focus();
});

// The most recent message with something to reply to. Opening a thread on
// nothing would be a key that appears broken.
useShortcutAction("thread", () => {
  const last = lastMessage();
  if (!last) return false;
  openThread(last.id);
});

useShortcutAction("editLast", () => editLast());

/*
 * Reacting to the latest message: its own quick row is opened, and the first
 * reaction in it focused, so the arrow keys and Enter finish the job — the
 * same row a pointer would use, rather than a second way of reacting.
 */
useShortcutAction("react", () => {
  const last = lastMessage();
  if (!last || !room.value?.member) return false;

  const row = document.getElementById(`message-${last.id}`);
  const add = row?.querySelector<HTMLButtonElement>(".reaction-add");
  if (!add) return false;

  add.click();
  void nextTick(() => row?.querySelector<HTMLButtonElement>(".quick-emoji")?.focus());
});

useShortcutAction("markRead", () => {
  markCaughtUp();
  scrollToLatest();
  void refreshRooms();
});

/*
 * Escape closes the thread beside the conversation — once somebody is out of
 * whatever field they were typing in. Leaving the field is the first Escape;
 * a thread closing under somebody mid-reply would lose their place.
 */
useShortcutAction("escape", (event) => {
  if (!openThreadId.value || isTyping(event)) return false;
  openThreadId.value = null;
});

/*
 * The up arrow in an empty message box edits the last thing the reader sent,
 * which is what every chat application they have used does. Only when the box
 * is empty: anywhere else the arrow moves the caret, and taking that away
 * would be stealing a key from somebody writing.
 */
const interfacePreferences = useInterfacePreferences();

function editLastFromComposer(event: KeyboardEvent) {
  if (draft.value !== "" || event.altKey || event.shiftKey || event.ctrlKey || event.metaKey) {
    return;
  }
  if (!interfacePreferences.shortcuts.value) return;
  if (editLast()) event.preventDefault();
}

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
  { label: $ts("room.photoOrVideo"), accept: "image/*,video/*", icon: ImageIcon },
  { label: $ts("room.audio"), accept: "audio/*", icon: Music },
  {
    label: $ts("room.document"),
    accept: ".pdf,.doc,.docx,.xls,.xlsx,.ppt,.pptx,.odt,.ods,.txt,.md",
    icon: FileText,
  },
  { label: $ts("room.anyFile"), accept: "", icon: Paperclip },
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
    // Through the injected fetcher rather than the browser's fetch: it takes an
    // absolute URL as readily as a server path, and routing it here keeps the
    // one rule — the layer does not fetch on its own — with no exception for
    // this. The URL is a public one a provider just returned, so no session is
    // involved; both clients simply fetch it.
    const bytes = await fetchMedia(url);

    // Named from the URL where possible, since the filename is what a reader
    // sees if the image itself will not display.
    const name = url.split("/").pop()?.split("?")[0] || "animation.gif";
    const file = new File([bytes], name.endsWith(".gif") ? name : `${name}.gif`, {
      type: "image/gif",
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

/**
 * What the last press said, keyed by message, for the live region.
 *
 * Kept here rather than in the button row because the outcome arrives as an
 * event over the live connection rather than as the answer to the press — so
 * what a reader is told is what actually happened, not what the client assumed
 * would happen.
 */
const pressOutcome = ref<Record<string, string>>({});

/**
 * Presses a button on a message.
 *
 * The refusals are worth telling apart. A conflict means somebody has already
 * answered — including this person, from another tab — and saying "try again"
 * to that would be wrong. Anything else is worth retrying.
 */
async function pressAction(
  message: { id: string; actions: MessageAction[]; controlsVersion: number },
  name: string,
  value?: string,
) {
  const action = message.actions.find((one) => one.name === name);
  if (!action) return;

  try {
    // The version on screen goes with the press, so a program that has since
    // replaced these controls has the press refused rather than miscounted.
    await roomsApi.pressAction(props.roomId, message.id, name, {
      value,
      version: message.controlsVersion,
    });
    pressOutcome.value = {
      ...pressOutcome.value,
      [message.id]: $ts("room.actionTaken", { label: action.label }),
    };
  } catch (cause) {
    const conflict = isApiError(cause) && cause.status === 409;
    // Two different conflicts, told apart by what the server said: an answer
    // already given, or a question that changed while it was being answered.
    const changed = conflict && /changed/.test(cause.message);
    pressOutcome.value = {
      ...pressOutcome.value,
      [message.id]: changed
        ? $ts("integrations.controlsChanged")
        : conflict
          ? $ts("room.actionAlreadyAnswered")
          : $ts("room.actionFailed"),
    };
  }
}

/**
 * The program a run of messages came from, when it was not a person.
 *
 * Read from the message's own content, where the server put it: a webhook's
 * or a bot's message carries its kind and name there, and nothing a client or
 * a sender supplies can add or remove it.
 */
function viaOf(group: { items: { content: unknown }[] }): MessageVia | undefined {
  return (group.items[0]?.content as { via?: MessageVia } | undefined)?.via;
}

/**
 * Programs' answers meant for this person alone, by the message they answer.
 *
 * Held only here: they are not part of the room's log and the server keeps no
 * copy, so leaving the conversation lets them go — which is what somebody
 * told "only you can see this" would expect.
 */
const privateReplies = ref<Record<string, PrivateReply[]>>({});

function dismissPrivate(messageId: string, replyId: string) {
  privateReplies.value = {
    ...privateReplies.value,
    [messageId]: (privateReplies.value[messageId] ?? []).filter((one) => one.id !== replyId),
  };
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
  // A program's latest replacement of a message's text and controls. The
  // highest version wins rather than the last to arrive, so a replay out of
  // order cannot put an older question back on screen.
  const updated = new Map<string, ControlsUpdate>();

  for (const message of messages.value) {
    if (message.type === "message.edited" && message.content.replaces) {
      replaced.set(message.content.replaces, message.content.body ?? "");
      continue;
    }
    if (message.type === "message.updated") {
      const update = message.content as unknown as ControlsUpdate;
      const previous = updated.get(update.updates);
      if (update.updates && (!previous || previous.version < update.version)) {
        updated.set(update.updates, update);
      }
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
    .map((message) => {
      const update = updated.get(message.id);
      return {
        ...message,
        attachments: message.attachments ?? [],
        body: update?.body ?? replaced.get(message.id) ?? message.content.body ?? "",
        edited: replaced.has(message.id),
        removed: redacted.has(message.id) || message.redacted,
        /*
         * The controls this message offers, as its program last left them.
         *
         * Dropped from a removed message: a question whose text is gone is not
         * a question anybody can answer, and leaving the controls would invite
         * somebody to answer something they can no longer read.
         */
        actions:
          redacted.has(message.id) || message.redacted
            ? []
            : (update?.actions ?? message.content.actions ?? []),
        // Which version of the controls is on screen, sent with a press.
        controlsVersion: update?.version ?? 0,
      };
    });
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
  return onDay(group.at);
}

const live = useLiveConnection({
  onEvent(eventRoomId, sequence, payload: LiveEventPayload) {
    if (eventRoomId !== roomId.value) return;
    if (payload.type === "call.started" || payload.type === "call.ended") {
      void refreshCall();
    }

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

  onPrivate(eventRoomId, reply) {
    if (eventRoomId !== roomId.value) return;
    privateReplies.value = {
      ...privateReplies.value,
      [reply.message]: [...(privateReplies.value[reply.message] ?? []), reply],
    };
  },

  // Who is in this conversation's call, for the bar that offers to join it.
  // The call itself — media and negotiation — is the call window's.
  onCallState: (state) => {
    if (state.roomId === roomId.value) roomCall.value = state.participants.length ? state : null;
  },
});

/*
 * This conversation's call, if there is one. A call's start or end arriving as
 * an event, or a change of conversation, is the cue to look again.
 */
const calling = useCall();
const roomCall = ref<Call | null>(null);
const { offers: serverOffers } = useServerInfo();
const callable = computed(
  () =>
    serverOffers("calls") &&
    calling.supported.value &&
    Boolean(room.value) &&
    !room.value?.homeServer &&
    !room.value?.archived,
);
const inThisCall = computed(() => calling.joined.value && calling.roomId.value === roomId.value);
async function refreshCall() {
  if (!callable.value) return;
  try {
    roomCall.value = await api.get<Call>(`/api/rooms/${roomId.value}/call`);
  } catch {
    roomCall.value = null;
  }
}
watch([callable, roomId], () => void refreshCall(), { immediate: true });

/*
 * Queued messages go out when the server is reachable again.
 *
 * Keyed on the live connection rather than on the browser's online event: that
 * one fires when an interface comes up, which is not the same as this server
 * answering. A flush against a network that is technically up and practically
 * useless would spend the queue's retries on nothing.
 */
/**
 * Sends what is waiting, if anything is.
 *
 * Kept separate from what triggers it because more than one thing should:
 * a queue that only flushes on one signal is a queue that sits full when that
 * signal does not come.
 */
async function flushWaiting() {
  if (!outbox.queue.value.length) return;

  await outbox.flush(async (message) => {
    const sent = await roomsApi.send(
      message.roomId,
      message.body,
      message.clientToken,
      message.attachmentIds,
    );

    // Only the conversation being looked at: the others reload their own
    // timelines when somebody opens them.
    if (
      message.roomId === roomId.value &&
      !messages.value.some((existing) => existing.id === sent.id)
    ) {
      messages.value.push(sent);
      scrollToLatest();
    }
  });
}

/*
 * Queued messages go out when the server is reachable again.
 *
 * Two signals, because either alone leaves the queue stuck. The live
 * connection reconnecting is the reliable one — it means this server is
 * actually answering — but a socket that never noticed the outage never
 * reconnects, and the queue then waits for a transition that will not come.
 * The browser's own online event covers that: it says the interface is back,
 * which is a weaker claim but arrives when the stronger one does not.
 *
 * Flushing twice costs nothing. The messages carry idempotency tokens, so a
 * resend returns the original rather than posting it again.
 */
watch(
  () => live.state.value,
  (now, before) => {
    if (now === "connected" && before !== "connected") void flushWaiting();
  },
);

useEventListener(window, "online", () => void flushWaiting());

async function load() {
  loading.value = true;
  try {
    room.value = await roomsApi.get(roomId.value);
    messages.value = await roomsApi.timeline(roomId.value);
    // Kept so the next visit with no connection has something to show.
    lastSeen.keep(roomId.value, messages.value, room.value);
    // Current again, so the notice goes.
    showingLastSeen.value = null;
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
    /*
     * An unreachable server is not a refused request.
     *
     * "Could not open this room" reads as a permission problem and sends
     * somebody looking for the wrong thing. Saying which of the two it was
     * costs one branch and saves that.
     */
    if (readFailure(cause) === "unreachable") {
      /*
        What this conversation looked like when the server was last reached.

        Shown rather than an empty room, and labelled rather than passed off as
        current: somebody replying to a stale timeline is answering a
        conversation that has moved on, which is worse than knowing they cannot
        see the latest.
      */
      const kept = lastSeen.of(roomId.value);
      if (kept) {
        messages.value = kept.messages;
        // The conversation too, so the header names it rather than showing a
        // blank where the channel should be.
        if (kept.room) room.value = kept.room;
        showingLastSeen.value = kept.at;
      }

      error.value = kept
        ? null
        : "You are offline. This conversation will load when the connection comes back.";
    } else {
      error.value = isApiError(cause) ? cause.message : "Could not open this room.";
    }
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

  const attachmentIds = ready.map((attachment) => attachment.id);

  try {
    /*
     * A room hosted elsewhere takes a different path, and cannot carry
     * attachments yet: those are stored by the server that holds the room,
     * and this one has nowhere to put them that the other could read.
     */
    if (room.value?.homeServer) {
      await roomsApi.sendRemote(roomId.value, body);
      uploads.clearFinished();
      pending.clear();
      // Nothing is added to the list here. The home server assigns the
      // message its position and sends it back, so what appears is what
      // everybody else sees rather than a local guess at it.
      return;
    }

    const sent = await roomsApi.send(roomId.value, body, clientToken, attachmentIds);
    uploads.clearFinished();
    pending.clear();
    if (!messages.value.some((message) => message.id === sent.id)) {
      messages.value.push(sent);
      scrollToLatest();
    }
    live.markRead(roomId.value, sent.sequence);
  } catch (cause) {
    /*
     * A server that could not be reached is not a message that failed.
     *
     * Somebody typing where there is no signal has written something they mean
     * to send. Restoring the text and apologising would make them retype it on
     * reconnection; holding it means it goes when the network comes back.
     * A refusal is different — the server read it and said no — and that is
     * reported rather than queued.
     */
    const held = outbox.holdIfUnsent(
      { clientToken, roomId: roomId.value, body, attachmentIds },
      cause,
    );

    if (held) {
      uploads.clearFinished();
      pending.clear();
      scrollToLatest();
    } else {
      // The text is restored, so a refusal does not lose what was typed.
      draft.value = previous;
      await nextTick();
      fitComposer();
      error.value = isApiError(cause) ? cause.message : "Could not send that message.";
    }
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
      label: $ts("room.copyText"),
      icon: Copy,
      action: () => navigator.clipboard?.writeText(message.body),
    });
  }

  if (room.value?.member && !message.removed) {
    items.push({
      label: $ts("room.replyInThread"),
      icon: MessageSquareText,
      action: () => openThread(message.id),
    });
    items.push({
      label: $ts("room.forward"),
      icon: Forward,
      action: () => {
        forwarding.value = [message.id];
      },
    });
    items.push({
      label: pins.pinnedIds.value.has(message.id) ? $ts("room.unpin") : $ts("room.pinToChannel"),
      icon: Pin,
      action: () => togglePin(message.id),
    });
    items.push({
      label: $ts("room.select"),
      icon: CheckSquare,
      action: () => toggleSelected(message.id),
    });
  }

  // Turning what was said into tracked work, for anybody who can read it —
  // whether they may file anywhere is the form's to find out.
  const fileIssue = issueLinks.menuItem(message);
  if (fileIssue) items.push(fileIssue);

  /*
   * When this message expires.
   *
   * Its sender's to set, or a moderator's on anybody's. Offered only while the
   * message is readable: putting a lifetime on something already gone would
   * do nothing, and offering it would suggest otherwise.
   */
  if ((mine || can("moderation.review")) && !message.removed) {
    items.push({
      label: message.lifetime ? $ts("room.changeWhenItExpires") : $ts("room.expireThisMessage"),
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
        label: anyCovered ? $ts("room.unmarkAsSensitive") : $ts("room.markAsSensitive"),
        icon: anyCovered ? Eye : EyeOff,
        action: () => setSensitive(markable, !anyCovered),
      });
    }
  }

  // Reporting somebody else's message, never one's own: there is nothing to
  // tell a moderator about a message you wrote and can delete yourself.
  if (!mine && !message.removed) {
    items.push({
      label: $ts("room.report"),
      icon: Flag,
      action: () => {
        reporting.value = { id: message.id, body: message.body };
      },
    });

    // Blocking the person, which is a personal boundary rather than a report:
    // nobody is told, least of all them.
    items.push({
      label: $ts("room.blockPerson", { who: shortName(sender) }),
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
        label: $ts("room.banPersonFromChannel", { who: shortName(sender) }),
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
      { label: $ts("action.edit"), icon: Pencil, action: () => startEditing(message) },
      {
        label: $ts("action.delete"),
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
    /*
     * A room hosted elsewhere is joined by asking that server, through ours.
     *
     * The refusal, when there is one, comes from a server we do not control —
     * so it is shown as it arrived rather than replaced with a guess about
     * what went wrong.
     */
    if (room.value?.homeServer) {
      await roomsApi.joinRemote(roomId.value);
      room.value = await roomsApi.get(roomId.value);
    } else {
      room.value = await roomsApi.join(roomId.value);
    }
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
  return atTime(value);
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
        <Button
          variant="ghost"
          size="icon"
          class="shrink-0 md:hidden"
          label="Channels"
          @click="navigationOpen = true"
        >
          <Menu class="size-4" />
        </Button>

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

        <!-- Which server holds this room, when it is not this one.

             Worth saying rather than hiding: a room hosted elsewhere is
             unavailable when that server is down, even though this one is
             running perfectly, and somebody meeting that for the first time
             deserves to have been told where the room actually lives. -->
        <span
          v-if="room?.homeServer"
          class="flex shrink-0 items-center gap-1.5 rounded-full bg-muted px-2 py-0.5 text-xs text-muted-foreground"
          :title="$ts('room.hostedElsewhereHint', { server: room.homeServer })"
        >
          <Globe class="size-3" aria-hidden="true" />
          {{ room.homeServer }}
        </span>

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

          <Button
            v-if="callable && !roomCall && !inThisCall"
            variant="ghost"
            size="icon"
            :label="$ts('calls.start')"
            :title="$ts('calls.start')"
            :disabled="calling.joining.value"
            @click="calling.join(roomId, false)"
          >
            <Phone class="size-3.5" />
          </Button>

          <ChannelSettings :room="room" :events="messages" @changed="room = $event" />

          <!-- Only for whoever moderates: a ban that can only be issued is a
               ban nobody reverses, so the way back in lives beside it. -->
          <Button
            variant="ghost"
            size="icon"
            v-if="can('moderation.restrict')"
            :label="showBans ? 'Hide bans' : 'Show who is banned'"
            :title="showBans ? 'Hide bans' : 'Show who is banned'"
            @click="showBans = !showBans"
          >
            <ShieldOff class="size-3.5" />
          </Button>
        </div>
      </header>

      <div v-if="showBans" class="shrink-0 border-b bg-muted/10 px-4 py-3">
        <ChannelBans :room-id="roomId" />
      </div>

      <CallBanner v-if="callable" :room-id="roomId" :call="roomCall" />

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
          {{
            selected.size === 1
              ? $t("room.oneMessageSelected")
              : $t("room.messagesSelected", { count: selected.size })
          }}
        </span>
        <Button variant="ghost" class="h-7 px-2" @click="forwarding = [...selected]">
          <Forward class="size-3.5" />
          {{ $t("room.forward") }}
        </Button>
        <Button variant="ghost" class="h-7 px-2" @click="copySelected">
          <Copy class="size-3.5" />
          {{ $t("action.copy") }}
        </Button>
        <Button variant="ghost" class="ml-auto h-7 px-2" @click="clearSelection">
          {{ $t("action.cancel") }}
        </Button>
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
        <span>{{ pull.refreshing.value ? $t("room.refreshing") : $t("room.pullToRefresh") }}</span>
      </div>

      <!-- What is on screen was true when the server was last reached, and is
           not being updated. Said plainly, because somebody replying to a
           stale conversation is answering something that has moved on. -->
      <div
        v-if="showingLastSeen"
        class="flex shrink-0 items-center justify-center gap-1.5 bg-warning/10 px-4 py-1.5 text-xs text-warning"
        role="status"
      >
        <CloudOff class="size-3.5 shrink-0" />
        <span
          >Showing this conversation as it was
          {{ when(new Date(showingLastSeen).toISOString()) }}</span
        >
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
            <p class="mt-3 text-sm font-medium">{{ $t("room.startOf") }} {{ room?.name }}</p>
            <p class="mt-0.5 text-xs text-muted-foreground">
              {{ room?.member ? $t("room.sendFirstMessage") : $t("room.joinToTakePart") }}
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
                <Avatar :name="viaOf(group)?.name ?? group.sender" size="sm" />
                <span class="text-xs font-semibold">
                  {{ group.mine ? "You" : (viaOf(group)?.name ?? shortName(group.sender)) }}
                </span>
                <!-- A program is named as one, in words, so nobody reads a
                     webhook's or a bot's message as a person's. -->
                <span
                  v-if="viaOf(group)"
                  class="rounded border border-border px-1 text-[0.625rem] font-medium tracking-wide text-muted-foreground uppercase"
                  data-sender-kind
                >
                  {{
                    viaOf(group)!.kind === "bot"
                      ? $t("integrations.bot")
                      : $t("integrations.webhook")
                  }}
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
                  <Button
                    variant="ghost"
                    size="icon"
                    class="md:hidden"
                    label="Message actions"
                    @click.stop="
                      contextMenu.show(
                        ($event.currentTarget as HTMLElement).getBoundingClientRect().left,
                        ($event.currentTarget as HTMLElement).getBoundingClientRect().bottom,
                        menuFor(message, group.mine, group.sender),
                      )
                    "
                  >
                    <MoreVertical class="size-4" />
                  </Button>
                  <Button
                    variant="ghost"
                    size="icon"
                    v-if="room?.member"
                    label="Reply in thread"
                    @click="openThread(message.id)"
                  >
                    <MessageSquareText class="size-3.5" />
                  </Button>
                  <Button
                    variant="ghost"
                    size="icon"
                    v-if="group.mine"
                    label="Edit"
                    @click="startEditing(message)"
                  >
                    <Pencil class="size-3.5" />
                  </Button>
                  <Button
                    variant="danger"
                    size="icon"
                    v-if="group.mine"
                    title="Delete"
                    @click="confirmDelete(message)"
                  >
                    <Trash2 class="size-3.5" />
                  </Button>
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
                    {{ $t("room.pinned") }}
                  </span>
                  <Bubble
                    v-if="message.body || message.removed"
                    :variant="message.removed ? 'redacted' : group.mine ? 'mine' : 'default'"
                    :align="group.mine ? 'end' : 'start'"
                    :continued="position > 0"
                  >
                    <form v-if="editing === message.id" @submit.prevent="saveEdit(message.id)">
                      <!-- Named, because a screen reader otherwise announces
                           this as an unlabelled text field: somebody editing
                           would be told they are typing into something, but
                           not what. There is no visible label to point at —
                           the box replaces the message in place — so the name
                           is carried on the control itself. -->
                      <Input
                        class="h-8"
                        :aria-label="$ts('room.editThisMessage')"
                        v-model="editDraft"
                        @keydown.esc="editing = null"
                        autofocus
                      />
                      <!-- Not dimmed at all.
                           
                           This hint sits inside the sender's own bubble, which
                           is a saturated ground: at 70% it measured 4.18:1 and
                           at 85% it was still 4.35, both under the 4.5 this
                           project holds itself to. Opacity was the wrong lever
                           — the fix is to let the text be the colour the theme
                           already guarantees against this background.
                           
                           It went unnoticed because the accessibility audit
                           never ran with a message open for editing: the hint
                           is only in the document then, so its silence was not
                           evidence anybody had looked. There is a check for
                           that state now. -->
                      <p class="mt-1 text-[0.6875rem]">
                        <Kbd class="border-current/30 bg-transparent text-current">Enter</Kbd>
                        to save,
                        <Kbd class="border-current/30 bg-transparent text-current">Esc</Kbd>
                        to cancel
                      </p>
                    </form>

                    <template v-else>
                      <template v-if="message.removed">{{ $t("room.messageRemoved") }}</template>
                      <MessageBody v-else :body="message.body" />
                      <!-- The buttons this message offers. Real buttons, so a
                           keyboard reaches them and a screen reader says
                           "button"; the outcome is announced rather than only
                           shown, because somebody who cannot see the row
                           change still has to be told the press landed. -->
                      <MessageActions
                        v-if="!message.removed && message.actions.length"
                        :key="`${message.id}-${message.controlsVersion}`"
                        :actions="message.actions"
                        :message-id="message.id"
                        :can-press="Boolean(room?.member)"
                        :outcome="pressOutcome[message.id] ?? ''"
                        @press="(name: string, value?: string) => pressAction(message, name, value)"
                      />
                      <!-- A program's answer to this reader alone. Marked as
                           such in words, not only by colour, because "only you
                           can see this" is the whole of what makes it
                           different from an ordinary reply. -->
                      <div
                        v-for="reply in privateReplies[message.id] ?? []"
                        :key="reply.id"
                        class="mt-2 flex items-start gap-2 rounded-md border border-dashed border-border bg-muted/40 px-2.5 py-1.5 text-sm"
                        role="note"
                        :aria-label="$ts('integrations.onlyYouCanSee')"
                      >
                        <EyeOff class="mt-0.5 size-3.5 shrink-0" aria-hidden="true" />
                        <div class="min-w-0 flex-1">
                          <p class="text-[0.6875rem] font-medium">
                            {{ $t("integrations.onlyYouCanSee") }}
                          </p>
                          <MessageBody :body="reply.body" />
                        </div>
                        <Button
                          variant="ghost"
                          size="icon"
                          class="size-6"
                          :label="$ts('integrations.dismiss')"
                          @click="dismissPrivate(message.id, reply.id)"
                        >
                          <X class="size-3.5" aria-hidden="true" />
                        </Button>
                      </div>
                      <!-- The issues this message became, that the reader may see. -->
                      <IssueLinks
                        v-if="!message.removed"
                        :issues="issueLinks.forMessage(message.id)"
                      />
                      <!-- Dimmed, but not below what can be read. At 60% this
                           fell under 4.5:1 against a sender's own bubble,
                           which axe caught and an eye would forgive until it
                           is the only thing saying a message was changed. -->
                      <span
                        v-if="message.edited && !message.removed"
                        class="ml-1 align-baseline text-[0.6875rem] opacity-85"
                      >
                        edited
                      </span>
                      <!-- Said on the message itself rather than on hover:
                           somebody deciding whether to answer needs to know it
                           is on a timer without going looking.

                           At 85% for the reason the edited marker is: 70% fell
                           under 4.5:1 against a sender's own bubble, which axe
                           caught only when a message with a lifetime happened
                           to be on screen during the audit. An eye forgives it
                           until it is the only thing saying a message is on a
                           timer. -->
                      <span
                        v-if="message.lifetime && !message.removed"
                        class="ml-1 inline-flex items-center gap-0.5 align-baseline text-[0.6875rem] opacity-85"
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

          <!-- Messages written while the server could not be reached.
               Shown rather than hidden: somebody who typed something needs to
               see that it exists and has not gone yet. Marked as waiting
               rather than styled like the rest, because a message that looks
               sent and is not is the worse of the two mistakes. -->
          <MessageGroup v-if="waiting.length" :mine="true">
            <div class="flex flex-row-reverse items-center gap-2 px-0.5">
              <Avatar :name="user?.address ?? ''" size="sm" />
              <span class="text-xs font-semibold">You</span>
              <span class="flex items-center gap-1 text-[0.6875rem] text-muted-foreground">
                <CloudOff class="size-3" />
                {{ $t("room.waitingToSend") }}
              </span>
            </div>

            <!-- A queued message arrives rather than appearing: it came from
                 somewhere, and one that materialises in place reads as the
                 interface redrawing itself. -->
            <UiArrive tag="div" class="contents">
              <MessageRow
                v-for="(message, position) in waiting"
                :key="message.clientToken"
                :mine="true"
              >
                <!-- Withdrawing is offered while it is still ours to withdraw.
                   Once it reaches the server it is a sent message, and taking
                   it back is a deletion other people may already have seen. -->
                <div class="group/waiting flex items-center gap-1.5">
                  <Button
                    variant="ghost"
                    class="size-7 shrink-0 p-0 opacity-0 transition-opacity group-hover/waiting:opacity-100 focus-visible:opacity-100"
                    :label="`Do not send: ${message.body}`"
                    @click="unsendWaiting(message.clientToken)"
                  >
                    <X class="size-3.5" />
                  </Button>

                  <div class="flex w-fit max-w-full flex-col items-end gap-1">
                    <Bubble variant="mine" align="end" :continued="position > 0" class="opacity-60">
                      <MessageBody :body="message.body" />
                    </Bubble>
                  </div>
                </div>
              </MessageRow>
            </UiArrive>
          </MessageGroup>
        </div>
      </div>

      <footer class="shrink-0 px-4 pt-1 pb-4">
        <p class="flex h-4 items-center gap-1.5 text-xs text-muted-foreground" aria-live="polite">
          <Transition name="motion-fade">
            <span v-if="typingLabel" class="motion-typing" aria-hidden="true">
              <span /><span /><span />
            </span>
          </Transition>
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
          <!-- Opened by the paperclip beside it rather than clicked directly,
               but still reachable by a screen reader — so it says what it is
               rather than announcing an unnamed file field. -->
          <input
            ref="filePicker"
            type="file"
            multiple
            aria-label="Choose files to attach"
            :accept="pickerAccept"
            class="sr-only"
            @change="pickFiles"
          />

          <!-- A menu rather than one button, so the picker can be told what to
               offer. A phone's file chooser is a different screen for photos,
               for the camera and for documents, and a bare "attach" opens the
               least useful of the three. -->
          <div class="relative shrink-0" data-composer-menu>
            <Button
              variant="ghost"
              size="icon"
              :disabled="room.archived"
              title="Attach"
              label="Attach"
              @click="attaching = !attaching"
            >
              <Paperclip class="size-4" />
            </Button>

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
          <Button
            size="icon"
            class="shrink-0"
            :variant="sendOriginal ? 'primary' : 'ghost'"
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
          </Button>
          <Button
            variant="ghost"
            size="icon"
            class="shrink-0"
            :disabled="room.archived || !draft.trim()"
            title="Send later"
            label="Send later"
            @click="scheduling = true"
          >
            <Clock class="size-4" />
          </Button>

          <div class="relative shrink-0" data-composer-menu>
            <Button
              variant="ghost"
              size="icon"
              :disabled="room.archived"
              title="Emoji"
              label="Emoji"
              @click="choosingEmoji = !choosingEmoji"
            >
              <Smile class="size-4" />
            </Button>

            <Transition name="motion-pop">
              <div
                v-if="choosingEmoji"
                class="absolute bottom-full left-0 z-20 mb-1 origin-bottom-left"
              >
                <EmojiPicker v-model="choosingEmoji" @pick="insertEmoji" @gif="attachGif" />
              </div>
            </Transition>
          </div>

          <textarea
            ref="composer"
            v-model="draft"
            rows="1"
            :disabled="room.archived"
            :placeholder="room.archived ? 'This room is archived' : `Message ${room.name}`"
            class="max-h-40 min-w-0 flex-1 resize-none bg-transparent px-2 py-1.5 text-sm outline-none placeholder:text-muted-foreground disabled:cursor-not-allowed"
            @input="
              announceTyping();
              fitComposer();
            "
            @keydown.enter.exact.prevent="send"
            @keydown.up="editLastFromComposer"
          />
          <Button
            size="icon"
            class="shrink-0"
            type="submit"
            :disabled="
              sending || room.archived || (!draft.trim() && uploads.completed.value.length === 0)
            "
            title="Send"
          >
            <Spinner v-if="sending" size="sm" />
            <SendHorizontal v-else class="size-4" />
          </Button>
        </form>

        <div
          v-else-if="room"
          class="flex items-center justify-between gap-4 rounded-xl border bg-surface px-3 py-2.5"
        >
          <p class="text-sm text-muted-foreground">{{ $t("room.joinToTakePart") }}</p>
          <Button @click="join">
            <Check class="size-3.5" />
            Join
          </Button>
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

    <IssueFromMessage
      v-if="issueLinks.filing.value"
      :room-id="roomId"
      :message="issueLinks.filing.value"
      @close="issueLinks.filing.value = null"
      @filed="issueLinks.refresh()"
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
      {{ $t("room.blockedNotTold") }}
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
    <Transition name="motion-toast">
      <p
        v-if="scheduled"
        class="fixed bottom-6 left-1/2 z-40 -translate-x-1/2 rounded-full bg-foreground px-4 py-2 text-xs font-medium text-background shadow-lg"
        role="status"
      >
        {{ $t("room.scheduledForLater") }}
      </p>
    </Transition>

    <Transition name="motion-toast">
      <p
        v-if="reported"
        class="fixed bottom-6 left-1/2 z-40 -translate-x-1/2 rounded-full bg-foreground px-4 py-2 text-xs font-medium text-background shadow-lg"
        role="status"
      >
        {{ $t("room.reportedToModerator") }}
      </p>
    </Transition>

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

    <Transition name="motion-panel">
      <ThreadPanel
        v-if="openThreadId && user"
        :room-id="roomId"
        :root-id="openThreadId"
        :self-address="user.address"
        @close="openThreadId = null"
        @replied="refreshThreadCounts"
      />
    </Transition>
  </div>
</template>

<style scoped>
@reference "#layers/ui/app/assets/css/main.css";

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

/* Somebody who turned motion down still sees which message it is: the mark is
   shown without fading, by a rule in the motion stylesheet keyed off the
   preference. */
</style>
