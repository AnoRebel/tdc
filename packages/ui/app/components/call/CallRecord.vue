<script setup lang="ts">
import { Circle, Square } from "lucide-vue-next";
import { Button } from "#layers/ui/app/components/ui/button";

/**
 * The recording control, for somebody the server lets record: start and stop,
 * then where the file went — into the conversation, or, when the server would
 * not take it, an offer to save it here.
 *
 * The recorder itself is loaded only when somebody starts.
 */

const { $ts } = useI18n();
const c = useCall();
const r = useCallRecording();
const { can } = useAuth();
const { offers } = useServerInfo();
const sendPiece = useUploadTransfer();
const saveFile = useSaveFile();

const guest = computed(() => Boolean(c.roomId.value && c.guestOf.value.includes(c.roomId.value)));
const supported = ref(false);
onMounted(async () => {
  supported.value = (await import("#layers/ui/app/lib/callRecorder")).canRecord();
});
const offered = computed(
  () => offers("calls.recording") && can("call.record") && !guest.value && supported.value,
);
const recording = computed(() => r.phase.value === "recording" || r.phase.value === "starting");
</script>

<template>
  <Button
    v-if="offered || recording"
    variant="ghost"
    size="icon"
    :disabled="r.phase.value === 'starting' || r.phase.value === 'saving' || c.onHold.value"
    :label="recording ? $ts('calls.record.stop') : $ts('calls.record.start')"
    :title="recording ? $ts('calls.record.stop') : $ts('calls.record.start')"
    :aria-pressed="recording"
    @click="recording ? r.stop() : r.start(sendPiece, saveFile)"
  >
    <Square v-if="recording" class="size-4 fill-destructive text-destructive" />
    <Circle v-else class="size-4 text-destructive" />
  </Button>
</template>
