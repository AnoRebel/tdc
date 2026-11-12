<script setup lang="ts">
import { Phone, Video } from "lucide-vue-next";
import { Button } from "#layers/ui/app/components/ui/button";

/**
 * Calling one person, from wherever they are shown.
 *
 * Opens the direct conversation with them, or the one the two already have,
 * goes there and rings them — the same as starting a call in that
 * conversation, so a call from a profile is not a different kind of call.
 * Absent where the server offers no calls or this device cannot make them.
 */

const props = defineProps<{ userId?: string; username?: string; name: string }>();
const { $ts, $localeRoute: localeRoute } = useI18n();
const { offers } = useServerInfo();
const rooms = useRooms();
const c = useCall();
const busy = ref(false);
const failed = ref(false);

async function call(withVideo: boolean) {
  busy.value = true;
  failed.value = false;
  try {
    const room = await rooms.openDirect(
      props.userId ? [props.userId] : [],
      props.username ? [props.username] : [],
    );
    await navigateTo(localeRoute({ name: "rooms-id", params: { id: room.id } }));
    await c.join(room.id, withVideo, { ring: true });
  } catch {
    failed.value = true;
  } finally {
    busy.value = false;
  }
}
</script>

<template>
  <span v-if="offers('calls') && c.supported.value" class="inline-flex items-center gap-1">
    <Button
      variant="ghost"
      size="icon"
      :disabled="busy"
      :label="$ts('calls.callPerson', { name })"
      :title="$ts('calls.callPerson', { name })"
      @click="call(false)"
    >
      <Phone class="size-3.5" />
    </Button>
    <Button
      variant="ghost"
      size="icon"
      :disabled="busy"
      :label="$ts('calls.videoCallPerson', { name })"
      :title="$ts('calls.videoCallPerson', { name })"
      @click="call(true)"
    >
      <Video class="size-3.5" />
    </Button>
    <span v-if="failed" class="text-xs text-destructive" role="alert">
      {{ $ts("calls.couldNotCall") }}
    </span>
  </span>
</template>
