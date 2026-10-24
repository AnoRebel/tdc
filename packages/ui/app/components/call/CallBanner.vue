<script setup lang="ts">
import { nameOf } from "#layers/ui/app/lib/names";
import type { Call } from "@tdc/types";
import { Maximize2, Mic, Phone, Play, TriangleAlert, Video } from "lucide-vue-next";
import { Button } from "#layers/ui/app/components/ui/button";

/**
 * The bar above a conversation that has a call going on.
 *
 * Says who is in it and offers to join, by voice or with video. When this
 * person is already in it, the bar says so and brings the call window back
 * instead — the call itself lives in that window, wherever they are.
 */

const props = defineProps<{ roomId: string; call: Call | null }>();
const { $t, $ts } = useI18n();
const c = useCall();

const here = computed(() => c.isIn(props.roomId));
// In it, but talking in another call, or holding every call.
const held = computed(() => here.value && c.holding.value.includes(props.roomId));
const names = computed(() =>
  (props.call?.participants ?? []).map((each) => nameOf(each)).join(", "),
);

const errorText = computed(() => {
  if (!c.error.value) return null;
  if (c.error.value === "permission") return $ts("calls.needPermission");
  if (c.error.value === "unreachable") return $ts("calls.couldNotJoin");
  return c.error.value;
});
</script>

<template>
  <div v-if="call || here || errorText" class="shrink-0 border-b" data-call-banner>
    <div
      v-if="call || here"
      class="flex flex-wrap items-center gap-2 bg-primary/5 px-4 py-2 text-sm"
    >
      <Phone class="size-4 text-primary" aria-hidden="true" />
      <span class="font-medium">
        {{
          held ? $t("calls.youAreHolding") : here ? $t("calls.youAreIn") : $t("calls.inProgress")
        }}
      </span>
      <span class="min-w-0 truncate text-muted-foreground" :title="names">{{ names }}</span>
      <span v-if="!here && c.metered.value" class="flex items-center gap-1 text-xs text-warning">
        <TriangleAlert class="size-3.5" aria-hidden="true" />
        {{ $t("calls.metered") }}
      </span>
      <div class="ml-auto flex gap-1.5">
        <Button v-if="held" size="small" variant="ghost" @click="c.switchTo(roomId)">
          <Play class="size-3.5" aria-hidden="true" />
          {{ $t("calls.resume") }}
        </Button>
        <Button v-else-if="here" size="small" variant="ghost" @click="c.view.value = 'expanded'">
          <Maximize2 class="size-3.5" aria-hidden="true" />
          {{ $t("calls.show") }}
        </Button>
        <template v-else>
          <Button
            size="small"
            variant="ghost"
            :disabled="c.joining.value"
            @click="c.join(roomId, false)"
          >
            <Mic class="size-3.5" aria-hidden="true" />
            {{ $t("calls.joinAudio") }}
          </Button>
          <Button size="small" :disabled="c.joining.value" @click="c.join(roomId, true)">
            <Video class="size-3.5" aria-hidden="true" />
            {{ $t("calls.joinVideo") }}
          </Button>
        </template>
      </div>
    </div>
    <p v-if="errorText" class="px-4 py-1.5 text-xs text-destructive" role="alert">
      {{ errorText }}
    </p>
  </div>
</template>
