<script setup lang="ts">
import { Languages } from "lucide-vue-next";

import { Button } from "#layers/ui/app/components/ui/button";

/**
 * Translating one conversation automatically, from its header.
 *
 * A toggle rather than a menu: it is on or off for this conversation, and
 * aria-pressed says which to somebody who cannot see the highlight. Where no
 * provider is usable yet the press goes to settings instead, since a toggle
 * that turned on and translated nothing would look broken.
 */

const props = defineProps<{ roomId: string }>();

const { $ts, $localeRoute: localeRoute } = useI18n();
const translation = useTranslation();

const on = computed(() => translation.autoFor(props.roomId));
const busy = ref(false);

async function toggle() {
  if (!translation.available.value) {
    await navigateTo({ ...localeRoute({ name: "settings" }), hash: "#translation" });
    return;
  }
  busy.value = true;
  try {
    await translation.setRoomAuto(props.roomId, !on.value);
  } catch {
    // Left as it was; the pressed state still tells the truth.
  } finally {
    busy.value = false;
  }
}
</script>

<template>
  <Button
    v-if="translation.offered.value && translation.overview.value"
    variant="ghost"
    size="icon"
    :class="on ? 'bg-accent text-accent-foreground' : ''"
    :label="
      !translation.available.value
        ? $ts('translation.setUp')
        : on
          ? $ts('translation.stopTranslatingConversation')
          : $ts('translation.translateConversation')
    "
    :aria-pressed="translation.available.value ? on : undefined"
    :disabled="busy"
    @click="toggle"
  >
    <Languages class="size-3.5" />
  </Button>
</template>
