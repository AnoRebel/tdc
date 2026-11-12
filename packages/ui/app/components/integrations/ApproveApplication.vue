<script setup lang="ts">
import { ShieldCheck, TriangleAlert } from "lucide-vue-next";

import type { Application, GrantablePermission, Room } from "@tdc/types";
import { Button } from "#layers/ui/app/components/ui/button";
import { Checkbox } from "#layers/ui/app/components/ui/checkbox";

/**
 * Approving an application, having been shown exactly what it will hold.
 *
 * Every permission is listed with what it allows, and anything that leaves the
 * server is stated before the button rather than after it. The button names
 * the count it grants, so nobody approves "an application" without having been
 * told what that means. What is approved is what was shown: the list goes back
 * to the server with the approval, and a request changed in between is
 * refused rather than granted unseen.
 */

const props = defineProps<{
  application: Application;
  grantable: GrantablePermission[];
  channels: Room[];
}>();
const emit = defineEmits<{ approve: [channels: string[]]; cancel: [] }>();

const { $t, $ts } = useI18n();
const chosen = ref<string[]>([]);

const described = computed(() =>
  props.application.requested.map(
    (name) =>
      props.grantable.find((permission) => permission.name === name) ?? {
        name,
        description: "",
        perChannel: false,
        administrative: false,
      },
  ),
);
const needsChannels = computed(() => described.value.some((permission) => permission.perChannel));

function toggle(id: string, on: boolean) {
  chosen.value = on ? [...chosen.value, id] : chosen.value.filter((one) => one !== id);
}
</script>

<template>
  <!-- eslint-disable-next-line vuejs-accessibility/no-static-element-interactions -- a dialog's backdrop, closed by Escape or a click outside -->
  <div
    class="fixed inset-0 z-50 flex items-center justify-center bg-background/70 p-4 backdrop-blur-sm"
    role="dialog"
    aria-modal="true"
    :aria-label="$ts('integrations.approveWhom', { name: props.application.name })"
    @click.self="emit('cancel')"
    @keydown.esc="emit('cancel')"
  >
    <div
      class="max-h-[90vh] w-[min(32rem,100%)] overflow-y-auto rounded-xl border border-border bg-card p-4 shadow-2xl"
    >
      <h2 class="flex items-center gap-2 text-sm font-semibold">
        <ShieldCheck class="size-4" aria-hidden="true" />
        {{ $t("integrations.approveWhom", { name: props.application.name }) }}
      </h2>
      <p class="mt-1 text-xs text-muted-foreground">{{ props.application.purpose }}</p>

      <h3 class="mt-4 text-xs font-medium">{{ $t("integrations.willHoldExactly") }}</h3>
      <ul class="mt-1 space-y-1" data-approval-permissions>
        <li v-for="permission in described" :key="permission.name" class="text-xs">
          <span class="font-mono font-medium">{{ permission.name }}</span>
          <span v-if="permission.administrative" class="ml-1 text-destructive">
            ({{ $t("integrations.administrative") }})
          </span>
          <span class="block text-muted-foreground">{{ permission.description }}</span>
        </li>
        <li v-if="!described.length" class="text-xs text-muted-foreground">
          {{ $t("integrations.holdsNothing") }}
        </li>
      </ul>
      <p class="mt-2 text-xs text-muted-foreground">{{ $t("integrations.notYourAuthority") }}</p>

      <!-- Said before the button, in words and with an icon, never only in
           colour: what leaves the server is the question an approver most
           needs answered. -->
      <div
        class="mt-3 rounded-md border p-2.5 text-xs"
        :class="props.application.sendsDataExternally ? 'border-warning bg-warning/10' : ''"
        data-approval-disclosure
      >
        <p class="flex items-center gap-1.5 font-medium">
          <TriangleAlert
            v-if="props.application.sendsDataExternally"
            class="size-3.5 text-warning"
            aria-hidden="true"
          />
          {{
            props.application.sendsDataExternally
              ? $t("integrations.sendsDataElsewhere")
              : $t("integrations.sendsNothingElsewhere")
          }}
        </p>
        <p v-if="props.application.sendsDataExternally" class="mt-1">
          {{ props.application.externalDisclosure }}
        </p>
      </div>

      <form class="mt-4 space-y-3" @submit.prevent="emit('approve', chosen)">
        <fieldset v-if="needsChannels">
          <legend class="mb-1.5 text-xs font-medium">{{ $t("integrations.actsIn") }}</legend>
          <div class="max-h-40 space-y-1 overflow-y-auto rounded-md border p-2">
            <!-- eslint-disable-next-line vuejs-accessibility/label-has-for -- the checkbox is a component, which the rule cannot see inside -->
            <label
              v-for="room in props.channels"
              :key="room.id"
              class="flex items-center gap-2 text-xs"
            >
              <Checkbox
                :model-value="chosen.includes(room.id)"
                @update:model-value="(on: boolean) => toggle(room.id, on)"
              />
              #{{ room.name }}
            </label>
          </div>
        </fieldset>

        <div class="flex justify-end gap-2">
          <Button variant="ghost" @click="emit('cancel')">{{ $t("action.cancel") }}</Button>
          <Button type="submit" :disabled="needsChannels && !chosen.length">
            {{ $t("integrations.approveCount", { count: described.length }) }}
          </Button>
        </div>
      </form>
    </div>
  </div>
</template>
