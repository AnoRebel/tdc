<script setup lang="ts">
import { MousePointer2 } from "lucide-vue-next";
import { Button } from "#layers/ui/app/components/ui/button";

/**
 * Remote control, from the sharer's side: who asks to control the screen,
 * with allow and decline, and while somebody does, who it is and how to take
 * it back — here, and in a small window above everything else on the screen,
 * which the desktop application shows.
 */

const { $t, $ts } = useI18n();
const r = useRemoteControl();
const key = computed(() => r.capability.value?.revokeKey ?? "");
</script>

<template>
  <div
    v-for="request in r.asking.value"
    :key="request.userId"
    class="space-y-1.5 rounded-md border border-warning/40 bg-warning/10 px-2 py-1.5 text-xs"
    role="alertdialog"
    :aria-label="$ts('calls.control.requestTitle', { name: request.name })"
  >
    <p class="flex items-center gap-1.5 font-medium">
      <MousePointer2 class="size-3.5" aria-hidden="true" />
      {{ $t("calls.control.requestTitle", { name: request.name }) }}
    </p>
    <p class="text-muted-foreground">{{ $t("calls.control.requestBody") }}</p>
    <div class="flex justify-end gap-1">
      <Button variant="ghost" size="small" @click="r.decline(request.userId)">
        {{ $t("calls.control.decline") }}
      </Button>
      <Button size="small" @click="r.allow(request.userId)">
        {{ $t("calls.control.allow") }}
      </Button>
    </div>
  </div>
  <div
    v-if="r.controller.value"
    class="flex items-center gap-2 rounded-md bg-destructive/10 px-2 py-1.5 text-xs text-destructive"
    role="status"
  >
    <MousePointer2 class="size-3.5 shrink-0" aria-hidden="true" />
    <span class="min-w-0 flex-1">
      <span class="font-medium">
        {{ $t("calls.control.controlledBy", { name: r.controller.value.name }) }}
      </span>
      <span class="block text-muted-foreground">
        {{ $t("calls.control.revokeHint", { key }) }}
      </span>
    </span>
    <Button variant="danger" size="small" @click="r.revoke()">
      {{ $t("calls.control.takeBack") }}
    </Button>
  </div>
</template>
