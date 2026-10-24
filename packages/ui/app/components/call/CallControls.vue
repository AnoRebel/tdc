<script setup lang="ts">
import { nameOf } from "#layers/ui/app/lib/names";
import {
  BellRing,
  Link2,
  Mic,
  MicOff,
  MonitorUp,
  MonitorX,
  Pause,
  PhoneOff,
  Play,
  Video,
  VideoOff,
} from "lucide-vue-next";
import { Button } from "#layers/ui/app/components/ui/button";

/** Microphone, camera, hold and leave, the same wherever the call is shown. */

defineProps<{ roomName?: string; inPicture?: boolean }>();
const { $t, $ts } = useI18n();
const c = useCall();
const picking = ref(false);
const linking = ref(false);
const { can } = useAuth();
const guest = computed(() => Boolean(c.roomId.value && c.guestOf.value.includes(c.roomId.value)));
</script>

<template>
  <p v-if="c.declined.value" class="text-center text-xs text-muted-foreground" role="status">
    {{
      c.declined.value.message
        ? $t("calls.declinedSaying", {
            name: nameOf(c.declined.value),
            message: c.declined.value.message,
          })
        : $t("calls.declinedNotice", { name: nameOf(c.declined.value) })
    }}
  </p>
  <p
    v-if="c.onHold.value"
    class="rounded-md bg-warning/10 px-2 py-1 text-center text-xs text-warning"
    role="status"
  >
    {{ $t("calls.youAreHolding") }}
  </p>
  <CallRingPicker v-if="picking" />
  <CallLinks v-if="linking" />
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
    <CallReact />
    <CallRecord />
    <Button
      v-if="c.canShareScreen.value"
      variant="ghost"
      size="icon"
      :disabled="Boolean(!c.screen.value && c.call.value?.presenter)"
      :label="c.screen.value ? $ts('calls.stage.stop') : $ts('calls.stage.share')"
      :title="
        !c.screen.value && c.call.value?.presenter
          ? $ts('calls.stage.someoneElse')
          : c.screen.value
            ? $ts('calls.stage.stop')
            : $ts('calls.stage.share')
      "
      :aria-pressed="Boolean(c.screen.value)"
      @click="c.screen.value ? c.stopSharing() : c.shareScreen()"
    >
      <MonitorX v-if="c.screen.value" class="size-4 text-destructive" />
      <MonitorUp v-else class="size-4" />
    </Button>
    <Button
      v-if="!guest && can('call.link')"
      variant="ghost"
      size="icon"
      :label="$ts('calls.links.title')"
      :title="$ts('calls.links.title')"
      :aria-pressed="linking"
      @click="linking = !linking"
    >
      <Link2 class="size-4" />
    </Button>
    <Button
      v-if="!guest"
      variant="ghost"
      size="icon"
      :label="$ts('calls.ringPeople')"
      :title="$ts('calls.ringPeople')"
      :aria-pressed="picking"
      @click="picking = !picking"
    >
      <BellRing class="size-4" />
    </Button>
    <Button
      variant="ghost"
      size="icon"
      :label="c.onHold.value ? $ts('calls.resume') : $ts('calls.hold')"
      :title="c.onHold.value ? $ts('calls.resume') : $ts('calls.hold')"
      :aria-pressed="c.onHold.value"
      @click="c.onHold.value && c.roomId.value ? c.switchTo(c.roomId.value) : c.hold()"
    >
      <Play v-if="c.onHold.value" class="size-4" />
      <Pause v-else class="size-4" />
    </Button>
    <Button variant="danger" size="small" @click="c.leave()">
      <PhoneOff class="size-3.5" aria-hidden="true" />
      {{ $t("calls.leave") }}
    </Button>
  </div>
</template>
