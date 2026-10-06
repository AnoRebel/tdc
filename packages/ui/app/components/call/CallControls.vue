<script setup lang="ts">
import { BellRing, Mic, MicOff, PhoneOff, Video, VideoOff } from "lucide-vue-next";
import { Button } from "#layers/ui/app/components/ui/button";

/** Microphone, camera and leave, the same wherever the call is shown. */

defineProps<{ roomName?: string; inPicture?: boolean }>();
const { $t, $ts } = useI18n();
const c = useCall();
const picking = ref(false);
</script>

<template>
  <p v-if="c.declined.value" class="text-center text-xs text-muted-foreground" role="status">
    {{ $t("calls.declinedNotice", { name: c.declined.value.username }) }}
  </p>
  <CallRingPicker v-if="picking" />
  <div class="flex items-center justify-center gap-2">
    <Button
      variant="ghost"
      size="icon"
      :label="c.microphone.value ? $ts('calls.mute') : $ts('calls.unmute')"
      :aria-pressed="!c.microphone.value"
      @click="c.toggleMicrophone()"
    >
      <Mic v-if="c.microphone.value" class="size-4" />
      <MicOff v-else class="size-4 text-destructive" />
    </Button>
    <Button
      variant="ghost"
      size="icon"
      :label="c.camera.value ? $ts('calls.cameraOff') : $ts('calls.cameraOn')"
      :aria-pressed="c.camera.value"
      @click="c.toggleCamera()"
    >
      <Video v-if="c.camera.value" class="size-4" />
      <VideoOff v-else class="size-4" />
    </Button>
    <Button
      variant="ghost"
      size="icon"
      :label="$ts('calls.ringPeople')"
      :title="$ts('calls.ringPeople')"
      :aria-pressed="picking"
      @click="picking = !picking"
    >
      <BellRing class="size-4" />
    </Button>
    <Button variant="danger" size="small" @click="c.leave()">
      <PhoneOff class="size-3.5" aria-hidden="true" />
      {{ $t("calls.leave") }}
    </Button>
  </div>
</template>
