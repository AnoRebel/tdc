<script setup lang="ts">
import { Button } from "#layers/ui/app/components/ui/button";

/**
 * Joining a call that is being recorded: who is recording it, said before
 * anything of the person's is sent, with the choice to join and be recorded
 * or not to join at all.
 */

const { $t } = useI18n();
const c = useCall();
const stay = useTemplateRef<InstanceType<typeof Button>>("stay");
onMounted(() => (stay.value?.$el as HTMLElement | undefined)?.focus());
</script>

<template>
  <div
    v-if="c.recordingNotice.value"
    class="fixed right-4 bottom-4 z-50 w-[min(22rem,calc(100vw-2rem))] space-y-3 rounded-xl border bg-background p-4 shadow-xl"
    role="alertdialog"
    aria-labelledby="call-recording-notice-title"
    aria-describedby="call-recording-notice-body"
    @keydown.esc="c.answerRecording(false)"
  >
    <p id="call-recording-notice-title" class="flex items-center gap-2 text-sm font-semibold">
      <span class="size-2 rounded-full bg-destructive" aria-hidden="true" />
      {{ $t("calls.record.noticeTitle") }}
    </p>
    <p id="call-recording-notice-body" class="text-xs text-muted-foreground">
      {{ $t("calls.record.noticeBody", { names: c.recordingNotice.value.names.join(", ") }) }}
    </p>
    <div class="flex justify-end gap-2">
      <Button variant="ghost" size="small" @click="c.answerRecording(false)">
        {{ $t("calls.record.leave") }}
      </Button>
      <Button ref="stay" size="small" @click="c.answerRecording(true)">
        {{ $t("calls.record.stay") }}
      </Button>
    </div>
  </div>
</template>
