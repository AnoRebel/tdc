<script setup lang="ts">
import type { UploadProgress } from "@tdc/types";
import { AlertCircle, Check, RotateCw, X } from "lucide-vue-next";

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
}>();

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
    class="mb-2 space-y-1.5"
    enter-active-class="transition duration-200 ease-out"
    enter-from-class="translate-y-1 opacity-0"
    leave-active-class="transition duration-150 ease-in"
    leave-to-class="opacity-0"
  >
    <div
      v-for="upload in shown"
      :key="upload.id"
      class="flex items-center gap-2.5 rounded-lg border bg-surface px-3 py-2"
      :class="upload.status === 'failed' && 'border-destructive/40 bg-destructive/8'"
    >
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
        <Check v-else-if="upload.status === 'done'" class="size-3.5 text-success" />
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
            <template v-if="upload.reduced"> reduced to {{ readableSize(upload.size) }} </template>
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

      <button
        v-if="upload.status === 'failed'"
        type="button"
        class="btn btn-ghost size-6 shrink-0 px-0"
        title="Try again"
        @click="emit('retry', upload.id)"
      >
        <RotateCw class="size-3.5" />
      </button>

      <button
        type="button"
        class="btn btn-ghost size-6 shrink-0 px-0"
        :title="upload.status === 'uploading' ? 'Cancel upload' : 'Remove'"
        @click="
          upload.status === 'uploading' || upload.status === 'waiting'
            ? emit('cancel', upload.id)
            : emit('dismiss', upload.id)
        "
      >
        <X class="size-3.5" />
      </button>
    </div>
  </TransitionGroup>
</template>
