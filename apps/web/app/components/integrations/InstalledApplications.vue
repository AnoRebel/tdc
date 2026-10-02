<script setup lang="ts">
import { Blocks } from "lucide-vue-next";

import type { InstalledApplication, Room } from "@tdc/types";
import { isApiError } from "@tdc/types";
import { Field } from "#layers/ui/app/components/ui/field";
import { Select } from "#layers/ui/app/components/ui/select";
import { onDayAtTime } from "#layers/ui/app/lib/dates";

/**
 * The applications installed on this server, for everybody.
 *
 * What each may do and when it was approved, and — for any channel somebody
 * can see — which of them can act there. Nobody needs administrator access to
 * find out what can read their conversations; it is theirs to know.
 */

const { $t, $ts } = useI18n();
const integrations = useIntegrations();
const rooms = useRooms();

const installed = ref<InstalledApplication[]>([]);
const channels = ref<Room[]>([]);
const roomId = ref<string | undefined>();
const here = ref<InstalledApplication[] | null>(null);
const error = ref<string | null>(null);

onMounted(async () => {
  try {
    const [applications, visible] = await Promise.all([integrations.installed(), rooms.list()]);
    installed.value = applications;
    channels.value = visible.filter((room) => room.kind === "channel" && !room.archived);
  } catch (cause) {
    error.value = isApiError(cause) ? cause.message : $ts("integrations.couldNotLoad");
  }
});

watch(roomId, async (id) => {
  here.value = null;
  if (!id) return;
  try {
    here.value = await integrations.inRoom(id);
  } catch (cause) {
    error.value = isApiError(cause) ? cause.message : $ts("integrations.couldNotLoad");
  }
});
</script>

<template>
  <section aria-labelledby="installed-applications-title" class="space-y-4">
    <div>
      <h2 id="installed-applications-title" class="flex items-center gap-1.5 text-sm font-semibold">
        <Blocks class="size-4" aria-hidden="true" />
        {{ $t("integrations.installedApplications") }}
      </h2>
      <p class="mt-1 text-xs text-muted-foreground">{{ $t("integrations.installedHint") }}</p>
    </div>

    <p
      v-if="error"
      class="rounded-md bg-destructive/10 px-3 py-2 text-sm text-destructive"
      role="alert"
    >
      {{ error }}
    </p>

    <p v-if="!installed.length" class="text-xs text-muted-foreground">
      {{ $t("integrations.noneInstalled") }}
    </p>
    <ul v-else class="space-y-2">
      <li v-for="application in installed" :key="application.id" class="rounded-md border p-3">
        <p class="text-sm font-medium">{{ application.name }}</p>
        <p class="mt-0.5 text-xs text-muted-foreground">{{ application.purpose }}</p>
        <p class="mt-1 font-mono text-[0.6875rem]">{{ application.permissions.join(", ") }}</p>
        <p class="mt-0.5 text-[0.6875rem] text-muted-foreground">
          {{ $t("integrations.approvedOn", { when: onDayAtTime(application.approved) }) }}
          · {{ $t("integrations.inChannels", { count: application.channelCount }) }}
        </p>
        <p v-if="application.sendsDataExternally" class="mt-0.5 text-[0.6875rem]">
          {{ $t("integrations.sendsDataElsewhere") }}: {{ application.externalDisclosure }}
        </p>
      </li>
    </ul>

    <Field :label="$ts('integrations.whatCanActIn')">
      <template #default="{ id }">
        <Select
          :id="id"
          v-model="roomId"
          class="max-w-xs"
          :label="$ts('integrations.whatCanActIn')"
          :placeholder="$ts('integrations.chooseChannel')"
          :options="channels.map((room) => ({ value: room.id, label: `#${room.name}` }))"
        />
      </template>
    </Field>
    <div v-if="here" role="status" data-applications-here>
      <p v-if="!here.length" class="text-xs text-muted-foreground">
        {{ $t("integrations.noApplicationsHere") }}
      </p>
      <ul v-else class="space-y-1 text-xs">
        <li v-for="application in here" :key="application.id">
          <span class="font-medium">{{ application.name }}</span> —
          <span class="font-mono">{{ application.permissions.join(", ") }}</span>
        </li>
      </ul>
    </div>
  </section>
</template>
