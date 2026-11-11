<script setup lang="ts">
import type { Attachment } from "@tdc/types";
import { ChevronLeft, ChevronRight, Download, FileQuestion, X } from "lucide-vue-next";

/**
 * An attachment, shown over the conversation.
 *
 * Opened in place rather than in a new tab, so that looking at a photograph
 * does not mean losing your place in what was being said about it.
 *
 * Nothing here renders uploaded markup. Images, video and audio are handed to
 * the elements built for them, and a PDF goes into a sandboxed frame — the
 * server already serves attachments inertly, and this is the second half of
 * that: uploaded content never reaches the application's session or storage.
 */

const props = defineProps<{
  attachments: Attachment[];
  /** Which one is open, by index. */
  startAt?: number;
}>();

const emit = defineEmits<{ close: [] }>();

const { $t, $ts } = useI18n();

const mediaUrl = useMediaUrl();
// Saved through the client, which on the desktop is the only one holding the session.
const download = useDownload();

const index = ref(props.startAt ?? 0);
const dialog = ref<HTMLElement | null>(null);

const current = computed(() => props.attachments[index.value]);
const hasSeveral = computed(() => props.attachments.length > 1);

/**
 * How a given attachment should be displayed.
 *
 * Taken from the server rather than derived from the content type here. Markdown
 * has no magic bytes and is detected as plain text, so the decision needs the
 * filename too — and making it once on the server means every client agrees.
 */
const kind = computed(() => current.value?.kind ?? "none");

function next() {
  if (hasSeveral.value) index.value = (index.value + 1) % props.attachments.length;
}

function previous() {
  if (hasSeveral.value) {
    index.value = (index.value - 1 + props.attachments.length) % props.attachments.length;
  }
}

/** Bytes, in the units a person thinks in. */
function readableSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(0)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

// Keyboard is the primary way out: a viewer that can only be dismissed by
// finding a small button is a trap for anyone not using a mouse.
onKeyStroke("Escape", () => emit("close"));
onKeyStroke("ArrowRight", next);
onKeyStroke("ArrowLeft", previous);

// Focus moves into the dialog on open and the page behind stops scrolling, so
// that the conversation does not drift while an image is being looked at.
onMounted(() => {
  dialog.value?.focus();
  document.body.style.overflow = "hidden";
});

onBeforeUnmount(() => {
  document.body.style.overflow = "";
});
</script>

<template>
  <Teleport to="body">
    <div
      ref="dialog"
      class="fixed inset-0 z-50 flex flex-col bg-black/85 backdrop-blur-sm"
      role="dialog"
      aria-modal="true"
      :aria-label="current?.filename"
      tabindex="-1"
      @click.self="emit('close')"
    >
      <header class="flex shrink-0 items-center gap-3 px-4 py-3 text-white">
        <div class="min-w-0 flex-1">
          <p class="truncate text-sm font-medium">{{ current?.filename }}</p>
          <p class="text-xs text-white/60">
            {{ readableSize(current?.size ?? 0) }}
            <span v-if="hasSeveral" class="tabular">
              · {{ $t("attachment.indexOfTotal", { index: index + 1, total: attachments.length }) }}
            </span>
          </p>
        </div>

        <a
          :href="mediaUrl(current?.url)"
          :download="current?.filename"
          @click.prevent="current && download(current.url, current.filename)"
          class="grid size-8 place-items-center rounded-md text-white/80 transition hover:bg-white/10 hover:text-white"
          :title="$ts('attachment.download')"
        >
          <Download class="size-4" />
        </a>
        <button
          type="button"
          class="grid size-8 place-items-center rounded-md text-white/80 transition hover:bg-white/10 hover:text-white"
          :aria-label="$ts('action.close')"
          @click="emit('close')"
        >
          <X class="size-4" />
        </button>
      </header>

      <div class="relative flex min-h-0 flex-1 items-center justify-center p-4">
        <button
          v-if="hasSeveral"
          type="button"
          class="absolute left-2 z-10 grid size-10 place-items-center rounded-full bg-black/40 text-white transition hover:bg-black/60"
          :aria-label="$ts('attachment.previousAttachment')"
          @click.stop="previous"
        >
          <ChevronLeft class="size-5" />
        </button>

        <!-- Stopping propagation so clicking the content itself does not
             dismiss the viewer; only the backdrop does. -->
        <img
          v-if="kind === 'image'"
          :src="mediaUrl(current!.url)"
          :alt="current!.filename"
          :width="current!.width"
          :height="current!.height"
          class="max-h-full max-w-full rounded-lg object-contain"
          @click.stop
        />

        <VideoClip
          v-else-if="kind === 'video'"
          :src="mediaUrl(current!.url)"
          :title="current!.filename"
          autoplay
          fill
          @click.stop
        />

        <AudioPlayer
          v-else-if="kind === 'audio'"
          :src="mediaUrl(current!.url)"
          :title="current!.filename"
          @click.stop
        />

        <!-- Text and markdown are fetched and rendered here rather than handed
             to the browser as a document: the server serves attachments
             inertly so nothing interprets them, and a frame would undo that. -->
        <TextViewer
          v-else-if="kind === 'text' || kind === 'markdown'"
          :src="mediaUrl(current!.url)"
          :filename="current!.filename"
          :kind="kind"
          :size="current!.size"
          class="max-h-full"
          @click.stop
        />

        <!-- Sandboxed with nothing granted: a PDF renderer is a large amount
             of machinery running over a file someone else supplied. -->
        <iframe
          v-else-if="kind === 'pdf'"
          :src="mediaUrl(current!.url)"
          :title="current!.filename"
          sandbox=""
          class="h-full w-full rounded-lg bg-white"
          @click.stop
        />

        <!-- Anything unrenderable still says what it is, rather than showing a
             broken frame. -->
        <div v-else class="text-center text-white" @click.stop>
          <span class="mx-auto grid size-14 place-items-center rounded-xl bg-white/10">
            <FileQuestion class="size-6" />
          </span>
          <p class="mt-3 font-medium">{{ current?.filename }}</p>
          <p class="mt-0.5 text-sm text-white/60">
            {{ current?.contentType }} · {{ readableSize(current?.size ?? 0) }}
          </p>
          <a
            :href="mediaUrl(current?.url)"
            :download="current?.filename"
            @click.prevent="current && download(current.url, current.filename)"
            class="btn btn-primary mt-4"
          >
            <Download class="size-3.5" />
            {{ $t("attachment.download") }}
          </a>
        </div>

        <button
          v-if="hasSeveral"
          type="button"
          class="absolute right-2 z-10 grid size-10 place-items-center rounded-full bg-black/40 text-white transition hover:bg-black/60"
          :aria-label="$ts('attachment.nextAttachment')"
          @click.stop="next"
        >
          <ChevronRight class="size-5" />
        </button>
      </div>
    </div>
  </Teleport>
</template>
