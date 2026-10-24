<script setup lang="ts">
import { PhoneOff } from "lucide-vue-next";
import { Button } from "#layers/ui/app/components/ui/button";

/**
 * Where the server offers calls and this device cannot place them: a call
 * button that says why instead of being missing for no reason anybody can see.
 */

const { $t, $ts } = useI18n();
const c = useCall();
const open = ref(false);
const reason = computed(() => {
  switch (c.unsupported.value) {
    case "insecure":
      return $ts("calls.unsupported.insecure");
    case "no-webrtc":
      return $ts("calls.unsupported.no-webrtc");
    case "no-webrtc-desktop":
      return $ts("calls.unsupported.no-webrtc-desktop");
    case "no-media":
      return $ts("calls.unsupported.no-media");
    case "gstreamer-webrtc":
      return $ts("calls.unsupported.gstreamer-webrtc");
  }
  return "";
});
</script>

<template>
  <span v-if="reason" class="relative">
    <Button
      variant="ghost"
      size="icon"
      :label="$ts('calls.unsupported.title')"
      :aria-expanded="open"
      @click="open = !open"
    >
      <PhoneOff class="size-3.5 text-muted-foreground" />
    </Button>
    <p
      v-if="open"
      class="absolute top-full right-0 z-30 mt-1 w-72 rounded-md border bg-background p-2 text-xs shadow-lg"
      role="status"
    >
      <span class="mb-1 block font-medium">{{ $t("calls.unsupported.title") }}</span>
      {{ reason }}
    </p>
  </span>
</template>
