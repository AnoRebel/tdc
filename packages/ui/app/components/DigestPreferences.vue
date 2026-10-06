<script setup lang="ts">
import { Mail } from "lucide-vue-next";
import { Button } from "#layers/ui/app/components/ui/button";
import { Select } from "#layers/ui/app/components/ui/select";
import { fully } from "#layers/ui/app/lib/dates";

import { isApiError } from "@tdc/types";
import type { DigestFrequency, DigestSetting } from "@tdc/types";

/**
 * Somebody's choice of an emailed summary instead of interruptions.
 *
 * Renders nothing on a server that cannot send one. Each change is saved as it
 * is made, as the other notification switches are, and the hour is always sent
 * with its zone: an hour alone means a different moment for everybody.
 */

const { $t, $ts } = useI18n();
const { available, read, choose } = useDigests();

const setting = ref<DigestSetting | null>(null);
const busy = ref(false);
const error = ref<string | null>(null);

const here = deviceTimeZone();

/** The zone the hour is in: what was chosen, or this device's for a first choice. */
const zone = computed(() => setting.value?.timeZone || here);

const frequencies = computed(() => [
  { value: "off" as DigestFrequency, label: $ts("digests.off") },
  {
    value: "daily" as DigestFrequency,
    label: $ts("digests.daily"),
    disabled: setting.value?.addressConfirmed === false,
  },
  {
    value: "weekly" as DigestFrequency,
    label: $ts("digests.weekly"),
    disabled: setting.value?.addressConfirmed === false,
  },
]);

// Hours as a 24-hour clock reads them, which needs no translating.
const hours = Array.from({ length: 24 }, (_, hour) => ({
  value: hour,
  label: `${String(hour).padStart(2, "0")}:00`,
}));

const weekdayKeys = [
  "sunday",
  "monday",
  "tuesday",
  "wednesday",
  "thursday",
  "friday",
  "saturday",
] as const;

const weekdays = computed(() =>
  weekdayKeys.map((key, index) => ({ value: index, label: $ts(`digests.weekday.${key}`) })),
);

async function load() {
  if (!available.value) return;
  error.value = null;
  try {
    setting.value = await read();
  } catch (cause) {
    error.value = isApiError(cause) ? cause.message : $ts("digests.couldNotLoad");
  }
}

/** Saves one change, carrying the rest of the setting as it stands. */
async function save(
  change: Partial<Pick<DigestSetting, "frequency" | "hour" | "weekday" | "timeZone">>,
) {
  if (!setting.value) return;
  error.value = null;
  busy.value = true;
  try {
    setting.value = await choose({
      frequency: change.frequency ?? setting.value.frequency,
      hour: change.hour ?? setting.value.hour,
      weekday: change.weekday ?? setting.value.weekday,
      timeZone: change.timeZone ?? zone.value,
    });
  } catch (cause) {
    error.value = isApiError(cause) ? cause.message : $ts("digests.couldNotSave");
  } finally {
    busy.value = false;
  }
}

const frequency = computed({
  get: () => setting.value?.frequency ?? "off",
  set: (value: DigestFrequency) => void save({ frequency: value }),
});
const hour = computed({
  get: () => setting.value?.hour ?? 8,
  set: (value: number) => void save({ hour: value }),
});
const weekday = computed({
  get: () => setting.value?.weekday ?? 1,
  set: (value: number) => void save({ weekday: value }),
});

watch(available, (now) => {
  if (now && !setting.value) void load();
});
onMounted(load);
</script>

<template>
  <section v-if="available" data-testid="digest-preferences">
    <h2 class="flex items-center gap-1.5 text-sm font-semibold">
      <Mail class="size-4" />
      {{ $t("digests.title") }}
    </h2>
    <p class="mt-1 text-xs text-muted-foreground">{{ $t("digests.hint") }}</p>

    <p
      v-if="error"
      class="mt-2 rounded-md bg-destructive/10 px-3 py-2 text-xs text-destructive"
      role="alert"
    >
      {{ error }}
    </p>

    <template v-if="setting">
      <p
        v-if="!setting.addressConfirmed && setting.frequency === 'off'"
        class="mt-2 text-xs text-muted-foreground"
      >
        {{ $t("digests.needsAddress") }}
      </p>

      <div class="mt-3 grid gap-3 sm:grid-cols-3">
        <label class="block text-xs font-medium">
          {{ $t("digests.howOften") }}
          <Select
            v-model="frequency"
            class="mt-1"
            :label="$ts('digests.howOften')"
            :options="frequencies"
            :disabled="busy"
          />
        </label>

        <label v-if="setting.frequency === 'weekly'" class="block text-xs font-medium">
          {{ $t("digests.day") }}
          <Select
            v-model="weekday"
            class="mt-1"
            :label="$ts('digests.day')"
            :options="weekdays"
            :disabled="busy"
          />
        </label>

        <label v-if="setting.frequency !== 'off'" class="block text-xs font-medium">
          {{ $t("digests.timeOfDay") }}
          <Select
            v-model="hour"
            class="mt-1"
            :label="$ts('digests.timeOfDay')"
            :options="hours"
            :disabled="busy"
          />
        </label>
      </div>

      <div v-if="setting.frequency !== 'off'" class="mt-2 space-y-1 text-xs text-muted-foreground">
        <p>
          {{ $t("digests.inZone", { zone }) }}
          <Button
            v-if="zone !== here"
            variant="ghost"
            class="ml-1 h-6 px-1.5 text-xs"
            :disabled="busy"
            @click="save({ timeZone: here })"
          >
            {{ $t("digests.useThisDevice", { zone: here }) }}
          </Button>
        </p>
        <p v-if="setting.nextAt">
          {{ $t("digests.next", { when: fully(setting.nextAt, zone) }) }}
        </p>
        <p v-if="setting.lastSentAt">
          {{ $t("digests.lastSent", { when: fully(setting.lastSentAt, zone) }) }}
        </p>
        <p>{{ $t("digests.replacesPush") }}</p>
      </div>

      <p v-if="busy" class="mt-2 text-xs text-muted-foreground" aria-live="polite">
        {{ $t("digests.saving") }}
      </p>
    </template>
  </section>
</template>
