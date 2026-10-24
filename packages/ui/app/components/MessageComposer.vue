<script setup lang="ts">
import type { RoomMember } from "@tdc/types";
import { isApiError } from "@tdc/types";
import {
  Clock,
  FileText,
  Image as ImageIcon,
  Maximize2,
  Music,
  Paperclip,
  SendHorizontal,
  Smile,
  Type as TypeIcon,
} from "lucide-vue-next";
import { Avatar } from "#layers/ui/app/components/ui/avatar";
import { Button } from "#layers/ui/app/components/ui/button";
import { Spinner } from "#layers/ui/app/components/ui/spinner";
import { formatField, messageKeys } from "#layers/ui/app/lib/composer-keys";
import type { Format } from "#layers/ui/app/lib/formatting";
import { completeMention, mentionAt, type MentionQuery } from "#layers/ui/app/lib/timeline";

/*
 * The box a message is written in, wherever messages are written.
 *
 * A channel and a thread both use this one, so a reply can carry everything a
 * message can — files picked, pasted or dropped, a GIF, formatting, a mention,
 * a time to send it later — and so a feature added to one cannot be missing
 * from the other. What happens on sending is the parent's: a channel may hold
 * a message in its outbox, a thread posts a reply.
 */
const props = defineProps<{
  roomId: string;
  /** The thread being replied in, when this is a thread's box. */
  threadRoot?: string;
  /** Where the unsent text is kept between visits. */
  draftKey: string;
  placeholder: string;
  disabled?: boolean;
  /**
   * Sends what was written. Rejects with the reason when the server refuses
   * it, which puts the text back; resolving means it was sent or is being held
   * to send, and either way the box is cleared.
   */
  send: (message: { body: string; attachmentIds: string[]; clientToken: string }) => Promise<void>;
  /** The up arrow in an empty box: edit the last thing sent. True if it did. */
  editLast?: () => boolean;
}>();

const emit = defineEmits<{ error: [message: string] }>();

const { $t, $ts } = useI18n();
const roomsApi = useRooms();
const interfacePreferences = useInterfacePreferences();
const typingPreference = useTypingPreference();
void typingPreference.load();
const scheduledMessages = useScheduled();

const draft = ref("");
const sending = ref(false);
const composer = ref<HTMLTextAreaElement | null>(null);
const filePicker = ref<HTMLInputElement | null>(null);

// Uploads are held here until the message that carries them is sent, so a
// refusal costs only the transfer and a failed send does not lose the file.
const uploads = useUploads();
const fetchMedia = useMediaFetch();
const drafts = useDrafts();

/** Files picked but not yet sent, kept so a retry needs no second selection. */
const pending = new Map<string, File>();

/*
 * What was being written here, restored — per conversation and per thread, so
 * switching between half-written messages finds each where it was left.
 */
watch(
  () => props.draftKey,
  (now, before) => {
    if (before && before !== now) drafts.keep(before, draft.value);
    draft.value = drafts.of(now);
    void nextTick().then(fitComposer);
  },
  { immediate: true },
);
watch(draft, (text) => drafts.keep(props.draftKey, text));

/** Grows the box with the message, up to a limit, then scrolls. */
function fitComposer() {
  const element = composer.value;
  if (!element) return;
  element.style.height = "auto";
  element.style.height = `${Math.min(element.scrollHeight, 160)}px`;
}

// Photographs are reduced before they leave the device, unless the sender
// asks for the original: on metered data the original costs them money for
// detail nobody sees.
const { resize, presets } = useImageResize();
const sendOriginal = ref(false);

const attaching = ref(false);
const choosingEmoji = ref(false);
const scheduling = ref(false);
const scheduled = ref(false);

watch(scheduled, (done) => {
  if (done) setTimeout(() => (scheduled.value = false), 4000);
});

// Only one menu at a time: two overlapping leave no way to tell which a click
// would reach.
watch(attaching, (open) => {
  if (open) choosingEmoji.value = false;
});
watch(choosingEmoji, (open) => {
  if (open) attaching.value = false;
});

function closeMenus(event: MouseEvent) {
  if (!attaching.value && !choosingEmoji.value) return;
  if ((event.target as HTMLElement).closest("[data-composer-menu]")) return;
  attaching.value = false;
  choosingEmoji.value = false;
}
useEventListener(document, "click", closeMenus);

/*
 * What the file picker should offer. A phone's chooser is a different screen
 * for photos, the camera and documents, and picks one from accept.
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

async function pickWith(accept: string) {
  attaching.value = false;
  pickerAccept.value = accept;
  // The attribute has to reach the element before it is opened.
  await nextTick();
  filePicker.value?.click();
}

/**
 * Starts uploading files, however they arrived — picked, pasted or dropped —
 * without blocking the conversation.
 */
async function addFiles(files: File[]) {
  if (props.disabled) return;
  for (const original of files) {
    // Anything that is not a reducible image comes back untouched.
    const file = sendOriginal.value ? original : await resize(original, presets.shared);
    void uploads
      .upload(props.roomId, file, original)
      .then((upload) => pending.set(upload.id, file));
  }
}

function pickFiles(event: Event) {
  const input = event.target as HTMLInputElement;
  const files = [...(input.files ?? [])];
  // Cleared so picking the same file twice in a row still fires a change.
  input.value = "";
  void addFiles(files);
}

/*
 * A pasted screenshot is a file, not text. Only files are taken: pasting
 * words still pastes words, and a paste carrying both keeps the words.
 */
function pasteFiles(event: ClipboardEvent) {
  const files = [...(event.clipboardData?.files ?? [])];
  if (!files.length) return;
  if (!event.clipboardData?.getData("text/plain")) event.preventDefault();
  void addFiles(files);
}

function retryUpload(id: string) {
  // The already-reduced file, so a retry does not repeat the work.
  const file = pending.get(id);
  if (file) void uploads.retry(props.roomId, id, file);
}

/** Writes a caption on one uploaded file, before the message is sent. */
async function captionAttachment(attachmentId: string, caption: string) {
  try {
    await roomsApi.captionAttachment(attachmentId, caption);
  } catch {
    // A caption that does not save is a smaller loss than interrupting
    // somebody mid-message to say so.
  }
}

/*
 * A chosen GIF, uploaded as an ordinary attachment rather than sent as a link,
 * so it is stored here and still there when the provider takes it down.
 */
async function attachGif(url: string) {
  try {
    const bytes = await fetchMedia(url);
    const name = url.split("/").pop()?.split("?")[0] || "animation.gif";
    const file = new File([bytes], name.endsWith(".gif") ? name : `${name}.gif`, {
      type: "image/gif",
    });
    await uploads.upload(props.roomId, file);
  } catch {
    emit("error", $ts("room.couldNotAttachGif"));
  }
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
  void nextTick(() => {
    element.focus();
    element.selectionStart = element.selectionEnd = start + emoji.length;
  });
}

const format = (chosen: Format) => formatField(composer.value, draft, chosen, fitComposer);

// The server expires an indication on its own, so this only has to be sent
// often enough to keep it alive while somebody is actually writing.
const announceTyping = useThrottleFn(() => {
  // Not announced at all by somebody who turned it off, rather than sent and
  // ignored: what is never sent cannot be shown.
  if (typingPreference.sends.value === false) return;
  void roomsApi.startTyping(props.roomId, props.threadRoot).catch(() => {});
}, 4000);

async function submit() {
  const body = draft.value.trim();
  const ready = uploads.completed.value;

  // A message may be nothing but its attachments.
  if ((!body && ready.length === 0) || sending.value || props.disabled) return;

  sending.value = true;
  closeMention();

  // A fresh token per message, so a retry after a lost response returns the
  // original rather than posting twice.
  const clientToken = crypto.randomUUID();
  const previous = draft.value;
  draft.value = "";
  await nextTick();
  fitComposer();

  try {
    await props.send({ body, attachmentIds: ready.map((file) => file.id), clientToken });
    uploads.clearFinished();
    pending.clear();
  } catch (cause) {
    // The text is restored, so a refusal does not lose what was typed.
    draft.value = previous;
    await nextTick();
    fitComposer();
    emit("error", isApiError(cause) ? cause.message : $ts("room.couldNotSend"));
  } finally {
    sending.value = false;
  }
}

/** Sends the message later rather than now — a reply stays a reply. */
async function scheduleDraft(sendAt: Date) {
  const body = draft.value.trim();
  scheduling.value = false;
  // A message may be nothing but its files, later as much as now.
  if (!body && uploads.completed.value.length === 0) return;

  try {
    await roomsApi.schedule(
      props.roomId,
      body,
      sendAt,
      uploads.completed.value.map((file) => file.id),
      props.threadRoot,
    );
    draft.value = "";
    uploads.clearFinished();
    scheduled.value = true;
    void scheduledMessages.refresh();
  } catch (cause) {
    emit("error", isApiError(cause) ? cause.message : $ts("room.couldNotSchedule"));
  }
}

/*
 * Suggesting people while an @name is typed.
 *
 * The list is the conversation's own members, read once when it is first
 * needed: suggesting somebody who is not here would be offering a mention that
 * reaches nobody, since only members are told.
 */
const members = ref<RoomMember[] | null>(null);
const mention = ref<MentionQuery | null>(null);
const highlighted = ref(0);
const listboxId = useId();

watch(
  () => props.roomId,
  () => (members.value = null),
);

const suggestions = computed(() => {
  if (!mention.value || !members.value) return [];
  const query = mention.value.query.toLowerCase();
  return members.value
    .filter(
      (member) =>
        member.username.toLowerCase().startsWith(query) ||
        member.displayName
          .toLowerCase()
          .split(/\s+/)
          .some((word) => word.startsWith(query)),
    )
    .slice(0, 6);
});

const suggesting = computed(() => suggestions.value.length > 0);

async function lookForMention() {
  const element = composer.value;
  if (!element) return;
  const found = mentionAt(draft.value, element.selectionStart ?? draft.value.length);
  if (found?.query !== mention.value?.query) highlighted.value = 0;
  mention.value = found;
  if (found && members.value === null) {
    try {
      members.value = await roomsApi.members(props.roomId);
    } catch {
      members.value = [];
    }
  }
}

function closeMention() {
  mention.value = null;
}

function choose(member: RoomMember) {
  const element = composer.value;
  if (!element || !mention.value) return;
  const next = completeMention(
    draft.value,
    mention.value,
    element.selectionStart ?? draft.value.length,
    member.username,
  );
  draft.value = next.text;
  closeMention();
  void nextTick(() => {
    element.focus();
    element.setSelectionRange(next.caret, next.caret);
    fitComposer();
  });
}

/** The keys the box answers to: the suggestion list's first, then a message box's. */
function keys(event: KeyboardEvent) {
  if (suggesting.value && !event.isComposing) {
    const count = suggestions.value.length;
    if (event.key === "ArrowDown" || event.key === "ArrowUp") {
      event.preventDefault();
      highlighted.value = (highlighted.value + (event.key === "ArrowDown" ? 1 : count - 1)) % count;
      return;
    }
    if (event.key === "Enter" || event.key === "Tab") {
      event.preventDefault();
      const member = suggestions.value[highlighted.value];
      if (member) choose(member);
      return;
    }
    if (event.key === "Escape") {
      // Closing the list is this Escape's whole job; the thread stays open.
      event.preventDefault();
      event.stopPropagation();
      closeMention();
      return;
    }
  }

  // The up arrow in an empty box edits the last thing sent, as every chat
  // application does — only when empty, so the arrow still moves the caret.
  if (
    event.key === "ArrowUp" &&
    draft.value === "" &&
    !event.altKey &&
    !event.shiftKey &&
    !event.ctrlKey &&
    !event.metaKey &&
    interfacePreferences.shortcuts.value &&
    props.editLast?.()
  ) {
    event.preventDefault();
    return;
  }

  messageKeys(event, composer.value, draft, () => void submit(), fitComposer);
}

defineExpose({
  focus: () => composer.value?.focus(),
  addFiles,
  /** Puts text back into an empty box — never over something half-typed. */
  restore(text: string) {
    if (draft.value.trim()) return;
    draft.value = text;
    void nextTick().then(fitComposer);
  },
});
</script>

<template>
  <div>
    <UploadTray
      :uploads="uploads.uploads.value"
      @cancel="uploads.cancel"
      @retry="retryUpload"
      @dismiss="uploads.remove"
      @caption="captionAttachment"
    />

    <FormattingToolbar
      v-if="!disabled && interfacePreferences.formattingToolbar.value"
      @format="format"
    />

    <form
      class="relative flex items-end gap-1 rounded-xl border bg-surface p-1.5 shadow-xs transition focus-within:border-ring focus-within:shadow-sm"
      @submit.prevent="submit"
    >
      <!-- People to mention, above the box, as a listbox the box controls:
           the arrows move through it and Enter or Tab chooses, without the
           caret ever leaving the text. -->
      <ul
        v-if="suggesting"
        :id="listboxId"
        role="listbox"
        :aria-label="$ts('room.mentionSomebody')"
        class="absolute right-0 bottom-full left-0 z-30 mb-1 overflow-hidden rounded-lg border border-border bg-card py-1 shadow-lg"
      >
        <li
          v-for="(member, index) in suggestions"
          :id="`${listboxId}-${index}`"
          :key="member.id"
          role="option"
          tabindex="-1"
          :aria-selected="index === highlighted"
          class="flex cursor-pointer items-center gap-2 px-3 py-1.5 text-sm"
          :class="index === highlighted ? 'bg-accent' : ''"
          @mousedown.prevent="choose(member)"
          @mousemove="highlighted = index"
        >
          <Avatar :name="member.address" size="sm" />
          <span class="truncate font-medium">{{ member.displayName }}</span>
          <span class="truncate text-xs text-muted-foreground">@{{ member.username }}</span>
        </li>
      </ul>

      <input
        ref="filePicker"
        type="file"
        multiple
        :aria-label="$ts('attachment.chooseFiles')"
        :accept="pickerAccept"
        class="sr-only"
        @change="pickFiles"
      />

      <div class="relative shrink-0" data-composer-menu>
        <Button
          variant="ghost"
          size="icon"
          :disabled="disabled"
          :label="$ts('room.attach')"
          :title="$ts('room.attach')"
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

      <!-- A per-message decision, so it sits where the file is picked. -->
      <Button
        size="icon"
        class="shrink-0 max-sm:hidden"
        :variant="sendOriginal ? 'primary' : 'ghost'"
        :disabled="disabled"
        :aria-pressed="sendOriginal"
        :label="sendOriginal ? $ts('room.sendingFullSize') : $ts('room.reducingImages')"
        :title="sendOriginal ? $ts('room.sendingFullSize') : $ts('room.reducingImages')"
        @click="sendOriginal = !sendOriginal"
      >
        <Maximize2 class="size-4" />
      </Button>
      <Button
        variant="ghost"
        size="icon"
        class="shrink-0"
        :disabled="disabled || (!draft.trim() && uploads.completed.value.length === 0)"
        :label="$ts('room.sendLater')"
        :title="$ts('room.sendLater')"
        @click="scheduling = true"
      >
        <Clock class="size-4" />
      </Button>

      <div class="relative shrink-0" data-composer-menu>
        <Button
          variant="ghost"
          size="icon"
          :disabled="disabled"
          :label="$ts('attachment.emoji')"
          :title="$ts('attachment.emoji')"
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

      <Button
        variant="ghost"
        size="icon"
        class="shrink-0 max-sm:hidden"
        :disabled="disabled"
        :aria-pressed="interfacePreferences.formattingToolbar.value"
        :label="
          interfacePreferences.formattingToolbar.value
            ? $ts('room.formatting.hideToolbar')
            : $ts('room.formatting.showToolbar')
        "
        :title="
          interfacePreferences.formattingToolbar.value
            ? $ts('room.formatting.hideToolbar')
            : $ts('room.formatting.showToolbar')
        "
        @click="
          interfacePreferences.formattingToolbar.value =
            !interfacePreferences.formattingToolbar.value
        "
      >
        <TypeIcon class="size-4" />
      </Button>

      <textarea
        ref="composer"
        v-model="draft"
        rows="1"
        :disabled="disabled"
        :placeholder="placeholder"
        :aria-label="placeholder"
        aria-autocomplete="list"
        :aria-controls="suggesting ? listboxId : undefined"
        :aria-activedescendant="suggesting ? `${listboxId}-${highlighted}` : undefined"
        class="max-h-40 min-w-0 flex-1 resize-none bg-transparent px-2 py-1.5 text-sm outline-none placeholder:text-muted-foreground disabled:cursor-not-allowed"
        @input="
          announceTyping();
          fitComposer();
          lookForMention();
        "
        @click="lookForMention"
        @blur="closeMention"
        @keydown="keys"
        @paste="pasteFiles"
      />
      <Button
        size="icon"
        class="shrink-0"
        type="submit"
        :disabled="sending || disabled || (!draft.trim() && uploads.completed.value.length === 0)"
        :label="$ts('action.send')"
        :title="$ts('action.send')"
      >
        <Spinner v-if="sending" size="sm" />
        <SendHorizontal v-else class="size-4" />
      </Button>
    </form>

    <ScheduleDialog
      v-if="scheduling"
      :body="draft"
      @cancel="scheduling = false"
      @schedule="scheduleDraft"
    />

    <!-- A scheduled message leaves no trace in the conversation, so without a
         word somebody would schedule it twice. -->
    <Transition name="motion-toast">
      <p
        v-if="scheduled"
        class="fixed bottom-6 left-1/2 z-40 -translate-x-1/2 rounded-full bg-foreground px-4 py-2 text-xs font-medium text-background shadow-lg"
        role="status"
      >
        {{ $t("room.scheduledForLater") }}
      </p>
    </Transition>
  </div>
</template>
