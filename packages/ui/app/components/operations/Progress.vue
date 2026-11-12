<script setup lang="ts">
import type { Operation } from "#layers/ui/app/composables/useOperations";
import { Button } from "#layers/ui/app/components/ui/button";

/**
 * How far a backup or restore has got, with a way to stop it.
 *
 * The figures are what the server last recorded, which it does every few
 * seconds; a table that takes a while shows no change until it is done.
 */
const props = defineProps<{ operation: Operation; cancellable?: boolean }>();
const emit = defineEmits<{ cancel: [] }>();

const { $t } = useI18n();

const percent = computed(() => {
  const { tablesDone = 0, tablesTotal = 0 } = props.operation.progress;
  return tablesTotal > 0 ? Math.round((tablesDone / tablesTotal) * 100) : 0;
});

function megabytes(bytes = 0) {
  return (bytes / (1024 * 1024)).toFixed(1);
}
</script>

<template>
  <div class="rounded-lg border bg-surface p-3" aria-live="polite">
    <div class="flex items-center justify-between gap-3">
      <p class="text-sm font-medium">
        {{ $t(`operations.kind.${props.operation.kind}`) }}
        <span class="text-muted-foreground"
          >· {{ $t(`operations.status.${props.operation.status}`) }}</span
        >
      </p>
      <Button
        v-if="props.cancellable && !props.operation.cancelRequested"
        variant="outline"
        size="small"
        @click="emit('cancel')"
      >
        {{ $t("operations.cancel") }}
      </Button>
    </div>
    <progress
      class="mt-2 h-2 w-full accent-primary"
      max="100"
      :value="percent"
      :aria-label="$ts('operations.progress')"
    />
    <p class="mt-1 text-xs text-muted-foreground">
      {{
        $t("operations.progressDetail", {
          tables: props.operation.progress.tablesDone ?? 0,
          total: props.operation.progress.tablesTotal ?? 0,
          files: props.operation.progress.files ?? 0,
          size: megabytes(props.operation.progress.bytes),
        })
      }}
      <span v-if="props.operation.progress.table" class="font-mono">{{
        props.operation.progress.table
      }}</span>
    </p>
  </div>
</template>
