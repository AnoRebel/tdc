<script setup lang="ts">
import type { UploadProgress } from "@tdc/types";
import { AlertCircle, Check, RotateCw, X } from "lucide-vue-next";
import { Spinner } from "#layers/ui/app/components/ui/spinner";
import { Button } from "#layers/ui/app/components/ui/button";

/**
 * Uploads in flight, shown above the composer.
 *
 * The bar reflects the transfer rather than a timer: a stalled upload shows a
 * number that stops moving, which is the honest thing to show and the thing
 * that tells someone to check their connection.
 */

const props = defineProps<{ uploads: UploadProgress[] }>();

const emit = defineEmits<{
  cancel: [id: string];
  retry: [id: string];
  dismiss: [id: string];
  caption: [attachmentId: string, caption: string];
}>();

/*
 * What the sender has written about each file, held here until they stop
 * typing.
 *
 * Sent on blur and on a pause rather than on every keystroke: a caption is a
 * sentence, and a request per character would be a request per character.
 */
const captions = ref<Record<string, string>>({});

const flush = useDebounceFn((attachmentId: string, caption: string) => {
  emit("caption", attachmentId, caption);
}, 500);

function onCaption(attachmentId: string, value: string) {
  captions.value = { ...captions.value, [attachmentId]: value };
  void flush(attachmentId, value);
}

/** Finished uploads leave the tray on their own; failures stay to be acted on. */
const shown = computed(() => props.uploads.filter((upload) => upload.status !== "cancelled"));

function percent(upload: UploadProgress): number {
  if (upload.status === "done") return 100;
  if (!upload.size) return 0;
  return Math.min(100, Math.round((upload.transferred / upload.size) * 100));
}

function readableSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(0)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}
</script>

<template>
  <TransitionGroup
    v-if="shown.length"
    tag="div"
    class="relative mb-2 space-y-1.5"
    name="motion-list"
  >
    <div
      v-for="upload in shown"
      :key="upload.id"
      class="rounded-lg border bg-surface px-3 py-2"
      :class="upload.status === 'failed' && 'border-destructive/40 bg-destructive/8'"
    >
      <div class="flex items-center gap-2.5">
        <span
          class="grid size-7 shrink-0 place-items-center rounded-md"
          :class="
            upload.status === 'failed'
              ? 'bg-destructive/15'
              : upload.status === 'done'
                ? 'bg-success/15'
                : 'bg-muted'
          "
        >
          <AlertCircle v-if="upload.status === 'failed'" class="size-3.5 text-destructive" />
          <Check v-else-if="upload.status === 'done'" class="size-3.5 text-success-text" />
          <Spinner v-else size="sm" class="text-muted-foreground" />
        </span>

        <div class="min-w-0 flex-1">
          <p class="flex items-baseline gap-2">
            <span class="truncate text-xs font-medium">{{ upload.filename }}</span>
            <span
              v-if="upload.status === 'uploading' || upload.status === 'waiting'"
              class="tabular shrink-0 text-[0.6875rem] text-muted-foreground"
            >
              {{ readableSize(upload.transferred) }} / {{ readableSize(upload.size) }}
            </span>

            <!-- What was actually sent. Someone who meant to share a screenshot
               at full size needs to see whether they did, without opening the
               file to check. -->
            <span
              v-else-if="upload.status === 'done'"
              class="shrink-0 text-[0.6875rem] text-muted-foreground"
            >
              <template v-if="upload.reduced">
                reduced to {{ readableSize(upload.size) }}
              </template>
              <template v-else>full size · {{ readableSize(upload.size) }}</template>
            </span>
          </p>

          <!-- The reason, not just the fact: a refusal names the limit it broke,
             which is what someone needs in order to do something about it. -->
          <p v-if="upload.status === 'failed'" class="mt-0.5 text-[0.6875rem] text-destructive">
            {{ upload.error }}
          </p>

          <div
            v-else-if="upload.status !== 'done'"
            class="mt-1 h-1 overflow-hidden rounded-full bg-muted"
            role="progressbar"
            :aria-valuenow="percent(upload)"
            aria-valuemin="0"
            aria-valuemax="100"
            :aria-label="`Uploading ${upload.filename}`"
          >
            <div
              class="h-full rounded-full bg-primary transition-[width] duration-200"
              :style="{ width: `${percent(upload)}%` }"
            />
          </div>
        </div>

        <Button
          variant="ghost"
          size="icon"
          class="shrink-0"
          v-if="upload.status === 'failed'"
          title="Try again"
          @click="emit('retry', upload.id)"
        >
          <RotateCw class="size-3.5" />
        </Button>

        <Button
          variant="ghost"
          size="icon"
          class="shrink-0"
          :title="upload.status === 'uploading' ? 'Cancel upload' : 'Remove'"
          @click="
            upload.status === 'uploading' || upload.status === 'waiting'
              ? emit('cancel', upload.id)
              : emit('dismiss', upload.id)
          "
        >
          <X class="size-3.5" />
        </Button>
      </div>

      <!-- A line about this file, which is per attachment rather than per
           message: sending several pictures with something to say about each
           is the ordinary case, and one message body cannot say which line
           describes which picture. -->
      <input
        v-if="upload.status === 'done' && upload.attachment"
        :value="captions[upload.attachment.id] ?? upload.attachment.caption ?? ''"
        type="text"
        class="mt-1.5 h-7 w-full rounded-md border border-border bg-transparent px-2 text-xs outline-none placeholder:text-muted-foreground focus:border-primary/50"
        :placeholder="`Say something about ${upload.filename}`"
        :aria-label="`Caption for ${upload.filename}`"
        @input="onCaption(upload.attachment.id, ($event.target as HTMLInputElement).value)"
        @blur="emit('caption', upload.attachment.id, ($event.target as HTMLInputElement).value)"
      />
    </div>
  </TransitionGroup>
</template>
