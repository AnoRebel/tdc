<script setup lang="ts">
import type { Attachment } from "@tdc/types";
import { Download, EyeOff, FileCode, FileText, Music, Play } from "lucide-vue-next";

/**
 * A message's attachments, as they appear in the conversation.
 *
 * Images show their preview rather than the original — a reduced copy of a few
 * kilobytes against several megabytes, which is the difference between a
 * readable conversation and an unusable one on metered mobile data. The full
 * file is fetched only when someone asks to see it.
 */

const props = defineProps<{ attachments: Attachment[] }>();

const { $t } = useI18n();

const mediaUrl = useMediaUrl();
// Saved through the client, which on the desktop is the only one holding the session.
const download = useDownload();
const { obscureSensitiveMedia } = useServerInfo();

const viewing = ref<number | null>(null);

/*
 * Which marked attachments this viewer has chosen to see.
 *
 * Per attachment and per session rather than remembered: revealing one image
 * is a decision about that image, and somebody who uncovered something
 * yesterday has not agreed to be shown everything today. It resets on reload,
 * which is the safe direction to be wrong in.
 */
const revealed = ref(new Set<string>());

/** Whether this attachment should currently be covered. */
function covered(attachment: Attachment): boolean {
  if (!attachment.sensitive || !obscureSensitiveMedia.value) return false;
  return !revealed.value.has(attachment.id);
}

function reveal(attachment: Attachment) {
  // A new Set rather than mutating: Vue does track Set mutations, but
  // replacing it keeps this obvious to read alongside the computed above.
  revealed.value = new Set(revealed.value).add(attachment.id);
}

/**
 * Where to fetch an attachment's preview.
 *
 * The server serves a placeholder for anything marked sensitive unless asked
 * otherwise, so revealing means fetching again with the parameter — not
 * removing a blur from something already downloaded. That is the whole point:
 * until this runs, the detail has not left the server.
 */
function previewSource(attachment: Attachment): string {
  const url = mediaUrl(attachment.previewUrl);
  if (!attachment.sensitive || covered(attachment)) return url;

  return `${url}${url.includes("?") ? "&" : "?"}reveal=1`;
}

/** Attachments the viewer can display, so arrows move between those only. */
const viewable = computed(() => props.attachments.filter((attachment) => attachment.renderable));

function open(attachment: Attachment) {
  const position = viewable.value.indexOf(attachment);
  viewing.value = position >= 0 ? position : null;
}

function readableSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(0)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

/** The icon for a file with no preview, so the type is legible at a glance. */
function iconFor(attachment: Attachment) {
  switch (attachment.kind) {
    case "video":
      return Play;
    case "audio":
      return Music;
    case "markdown":
      return FileText;
    case "text":
      return FileCode;
    default:
      return FileText;
  }
}

/**
 * The space a preview will occupy, so the conversation does not jump as images
 * arrive. Falls back to a modest box when the server did not record dimensions.
 */
function previewStyle(attachment: Attachment) {
  if (!attachment.width || !attachment.height) return {};

  return {
    aspectRatio: `${attachment.width} / ${attachment.height}`,
    maxWidth: "min(20rem, 100%)",
  };
}
</script>

<template>
  <div v-if="attachments.length" class="mt-1.5 flex flex-wrap gap-2">
    <!-- Each attachment and its own caption travel together, so several sent
         at once each carry their own line rather than sharing the message
         body — which cannot say which line describes which picture. -->
    <div
      v-for="attachment in attachments"
      :key="attachment.id"
      class="flex max-w-full flex-col gap-1"
    >
      <!-- Media somebody marked sensitive, covered until this viewer asks for
           it. The file is still fetchable by anybody who may read the room:
           this is about not showing somebody something they did not ask to
           see, not about who may see it. -->
      <button
        v-if="covered(attachment)"
        type="button"
        class="group/cover relative overflow-hidden rounded-lg border border-dashed bg-surface-sunken transition hover:border-border-strong"
        :style="previewStyle(attachment)"
        @click="reveal(attachment)"
      >
        <!--
          What arrives here is already obscured by the server: the image
          reduced to a handful of pixels and scaled back up, with the detail
          averaged away rather than hidden. The CSS blur on top is presentation
          — it softens the blocks into something that looks deliberate — and
          removing it in the developer tools reveals only the placeholder,
          because the detail was never sent.

          scale-110 hides the transparent edge a blur leaves at the border of
          its own element.
        -->
        <img
          v-if="attachment.previewUrl"
          :src="mediaUrl(attachment.previewUrl)"
          alt=""
          aria-hidden="true"
          loading="lazy"
          decoding="async"
          class="size-full scale-110 object-cover blur-md"
        />

        <span
          class="absolute inset-0 flex flex-col items-center justify-center gap-1.5 bg-background/45 px-4 text-center"
        >
          <!-- shrink-0: the label is a flex column inside a box whose height
               comes from the image's aspect ratio, so without it the icon is
               the child squeezed to nothing. -->
          <EyeOff class="size-5 shrink-0 drop-shadow" />
          <span class="text-xs font-medium drop-shadow">{{
            $t("attachment.markedSensitive")
          }}</span>
          <span class="text-[0.6875rem] opacity-80 drop-shadow">
            {{ $t("attachment.clickToShow") }}
          </span>
        </span>
      </button>

      <template v-else>
        <!-- An animation, played where it sits. It has no still preview by
           design: a thumbnail is a frozen first frame, and showing that in
           place of the animation removes the only thing about a GIF that
           matters. -->
        <button
          v-if="attachment.animated"
          type="button"
          class="group/att relative overflow-hidden rounded-lg border bg-surface-sunken transition hover:border-border-strong"
          :style="previewStyle(attachment)"
          :title="attachment.filename"
          @click="open(attachment)"
        >
          <img
            :src="mediaUrl(attachment.url)"
            :alt="attachment.caption || attachment.filename"
            loading="lazy"
            decoding="async"
            class="size-full object-cover"
          />
          <!-- Said plainly, because an animation that has not started loading
             looks like a still image that failed to load. -->
          <span
            class="absolute top-1 left-1 rounded bg-black/60 px-1 py-0.5 text-[0.5625rem] font-semibold tracking-wide text-white uppercase"
          >
            GIF
          </span>
        </button>

        <!-- An image with a preview: shown, and opened at full size on demand. -->
        <button
          v-else-if="attachment.previewUrl"
          type="button"
          class="group/att relative overflow-hidden rounded-lg border bg-surface-sunken transition hover:border-border-strong"
          :style="previewStyle(attachment)"
          :title="attachment.filename"
          @click="open(attachment)"
        >
          <img
            :src="previewSource(attachment)"
            :alt="attachment.filename"
            loading="lazy"
            decoding="async"
            class="size-full object-cover"
          />
          <span
            class="absolute inset-x-0 bottom-0 truncate bg-gradient-to-t from-black/70 to-transparent px-2 pt-6 pb-1.5 text-left text-[0.6875rem] text-white opacity-0 transition-opacity group-hover/att:opacity-100"
          >
            {{ attachment.filename }}
          </span>
        </button>

        <!-- Audio and video play where they sit, so listening to a voice note or
           watching a screen recording does not take over the conversation.
           Enlarging is offered for when the detail matters more. -->
        <AudioPlayer
          v-else-if="attachment.kind === 'audio'"
          :src="mediaUrl(attachment.url)"
          :title="attachment.filename"
          expandable
          @expand="open(attachment)"
        />

        <VideoPlayer
          v-else-if="attachment.kind === 'video'"
          :src="mediaUrl(attachment.url)"
          :title="attachment.filename"
          :width="attachment.width"
          :height="attachment.height"
          expandable
          @expand="open(attachment)"
        />

        <!-- A document or text file: opened in the viewer, where there is room
           to read it. -->
        <button
          v-else-if="attachment.renderable"
          type="button"
          class="flex items-center gap-2.5 rounded-lg border bg-surface px-3 py-2 text-left transition hover:border-border-strong hover:bg-accent"
          @click="open(attachment)"
        >
          <span class="grid size-8 shrink-0 place-items-center rounded-md bg-primary/12">
            <component :is="iconFor(attachment)" class="size-4 text-primary" />
          </span>
          <span class="min-w-0">
            <span class="block max-w-52 truncate text-sm font-medium">
              {{ attachment.filename }}
            </span>
            <span class="block text-xs text-muted-foreground">
              {{ readableSize(attachment.size) }}
            </span>
          </span>
        </button>

        <!-- Everything else: name, size and type, with a download. Never an
           error and never an empty frame. -->
        <a
          v-else
          :href="mediaUrl(attachment.url)"
          :download="attachment.filename"
          @click.prevent="download(attachment.url, attachment.filename)"
          class="flex items-center gap-2.5 rounded-lg border bg-surface px-3 py-2 transition hover:border-border-strong hover:bg-accent"
        >
          <span class="grid size-8 shrink-0 place-items-center rounded-md bg-muted">
            <component :is="iconFor(attachment)" class="size-4 text-muted-foreground" />
          </span>
          <span class="min-w-0">
            <span class="block max-w-52 truncate text-sm font-medium">
              {{ attachment.filename }}
            </span>
            <span class="block text-xs text-muted-foreground">
              {{ readableSize(attachment.size) }}
            </span>
          </span>
          <Download class="size-3.5 shrink-0 text-muted-foreground" />
        </a>
      </template>

      <!-- The caption stays visible under a covered attachment: it is the
           sender's own words about what they sent, and it is often the thing
           that tells a viewer whether to uncover it. -->
      <p v-if="attachment.caption" class="max-w-full text-xs text-muted-foreground">
        {{ attachment.caption }}
      </p>
    </div>

    <AttachmentViewer
      v-if="viewing !== null"
      :attachments="viewable"
      :start-at="viewing"
      @close="viewing = null"
    />
  </div>
</template>
