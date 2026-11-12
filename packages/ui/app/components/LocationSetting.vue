<script setup lang="ts">
import { useForm } from "@tanstack/vue-form";
import type { AnyFieldApi } from "@tanstack/vue-form";
import { usePermission } from "@vueuse/core";
import { LocateFixed, MapPin, PenLine } from "lucide-vue-next";
import * as v from "valibot";

import type { DirectoryVisibility, LocationMode, OwnLocation } from "@tdc/types";
import { isApiError } from "@tdc/types";
import { Button } from "#layers/ui/app/components/ui/button";
import { Field } from "#layers/ui/app/components/ui/field";
import { Input } from "#layers/ui/app/components/ui/input";
import VisibilityPicker from "#layers/ui/app/components/directory/VisibilityPicker.vue";
import { fully } from "#layers/ui/app/lib/dates";
import { currentPosition } from "#layers/ui/app/lib/locationrefresh";

/**
 * Where somebody is, in settings: typed, or detected by their device, and who
 * may see it.
 *
 * One setting for the account, shown on their directory profile and in the
 * member lists they appear in. Hidden until they choose otherwise.
 *
 * Detection asks the browser for a position once, here, and sends it to this
 * server, which names the nearest town from a list it carries. Only the town is
 * kept. Where the browser cannot or will not say — permission refused, or a
 * desktop webview with no location service — this says so and offers typing it
 * instead.
 */

const { $t, $ts } = useI18n();
const api = useApi();
const { say } = useNotice();

const PLACE_LIMIT = 80;

const own = ref<OwnLocation | null>(null);
const error = ref<string | null>(null);
/** Why detection did not work, in words somebody can act on. */
const problem = ref<string | null>(null);
const detecting = ref(false);

const permission = usePermission("geolocation");
const geolocationAvailable = import.meta.client && "geolocation" in navigator;

const schema = computed(() =>
  v.object({
    place: v.pipe(
      v.string(),
      v.trim(),
      v.maxLength(PLACE_LIMIT, $ts("location.tooLong", { count: PLACE_LIMIT })),
    ),
    visibility: v.picklist(["public", "members", "hidden"]),
  }),
);

const form = useForm({
  defaultValues: { place: "", visibility: "hidden" as DirectoryVisibility },
  validators: { onSubmit: schema.value },
  async onSubmit({ value }) {
    await save(mode.value, value.place.trim(), value.visibility);
  },
});

const mode = computed<LocationMode>(() => own.value?.mode ?? "manual");

function adopt(location: OwnLocation) {
  own.value = location;
  form.reset({ place: location.place, visibility: location.visibility });
}

onMounted(async () => {
  try {
    adopt(await api.get<OwnLocation>("/api/me/location"));
  } catch (cause) {
    error.value = isApiError(cause) ? cause.message : $ts("location.couldNotLoad");
  }
});

async function save(next: LocationMode, place: string, visibility: DirectoryVisibility) {
  error.value = null;
  try {
    adopt(await api.put<OwnLocation>("/api/me/location", { mode: next, place, visibility }));
    say($ts("location.saved"));
  } catch (cause) {
    error.value = isApiError(cause) ? cause.message : $ts("location.couldNotSave");
  }
}

/*
 * Asks the device where it is, once, and lets the server name the town.
 *
 * The browser's own prompt is the consent: this is only ever called from a
 * button somebody pressed. Refreshing later happens elsewhere and never
 * prompts.
 */
async function detect() {
  problem.value = null;
  error.value = null;
  if (!geolocationAvailable) {
    problem.value = $ts("location.unavailable");
    return;
  }
  detecting.value = true;
  try {
    const { coords } = await currentPosition();
    adopt(
      await api.post<OwnLocation>("/api/me/location/detect", {
        latitude: coords.latitude,
        longitude: coords.longitude,
      }),
    );
    say($ts("location.detected", { place: own.value?.place ?? "" }));
  } catch (cause) {
    if (isApiError(cause)) {
      problem.value = cause.code === "not-found" ? $ts("location.noTownNearby") : cause.message;
    } else if ((cause as GeolocationPositionError)?.code === 1) {
      problem.value = $ts("location.denied");
    } else {
      problem.value = $ts("location.couldNotDetect");
    }
  } finally {
    detecting.value = false;
  }
}

/** Switching to typing keeps the last detected place as the starting text. */
function setItMyself() {
  problem.value = null;
  if (mode.value === "manual") return;
  void save("manual", own.value?.place ?? "", form.state.values.visibility);
}

/** The first thing wrong with a field, once somebody has touched it. */
function firstError(field: AnyFieldApi): string | undefined {
  if (!field.state.meta.isTouched && !form.state.submissionAttempts) return undefined;
  const [issue] = field.state.meta.errors;
  if (!issue) return undefined;
  return typeof issue === "string" ? issue : (issue as { message?: string }).message;
}
</script>

<template>
  <section id="location" class="scroll-mt-4" data-location-setting>
    <h2 class="flex items-center gap-1.5 text-sm font-semibold">
      <MapPin class="size-4" />
      {{ $t("location.title") }}
    </h2>
    <p class="mt-1 text-xs text-muted-foreground">{{ $t("location.hint") }}</p>

    <p
      v-if="error"
      class="mt-3 rounded-md bg-destructive/10 px-3 py-2 text-sm text-destructive"
      role="alert"
    >
      {{ error }}
    </p>

    <div
      class="mt-3 inline-flex rounded-lg border p-0.5"
      role="group"
      :aria-label="$ts('location.howKept')"
    >
      <Button
        :variant="mode === 'manual' ? 'primary' : 'ghost'"
        :aria-pressed="mode === 'manual'"
        class="h-7 px-2.5 text-xs"
        @click="setItMyself"
      >
        <PenLine class="size-3.5" />
        {{ $t("location.manual") }}
      </Button>
      <Button
        :variant="mode === 'automatic' ? 'primary' : 'ghost'"
        :aria-pressed="mode === 'automatic'"
        class="h-7 px-2.5 text-xs"
        :disabled="detecting"
        @click="detect"
      >
        <LocateFixed class="size-3.5" />
        {{ $t("location.automatic") }}
      </Button>
    </div>

    <p
      v-if="problem"
      class="mt-2 rounded-md border border-warning/40 bg-warning/10 px-3 py-2 text-xs"
      role="status"
      data-location-problem
    >
      {{ problem }}
      <button
        v-if="mode === 'automatic'"
        type="button"
        class="ml-1 underline underline-offset-2"
        @click="setItMyself"
      >
        {{ $t("location.typeInstead") }}
      </button>
    </p>

    <form
      class="mt-4 space-y-4"
      :aria-label="$ts('location.title')"
      @submit.prevent="form.handleSubmit()"
    >
      <form.Field v-if="mode === 'manual'" name="place">
        <template #default="{ field }">
          <Field
            :label="$ts('location.place')"
            :hint="$ts('location.placeHint')"
            :error="firstError(field)"
          >
            <template #default="{ id, invalid, describedBy }">
              <Input
                :id="id"
                class="max-w-sm"
                :model-value="field.state.value"
                autocomplete="address-level2"
                :placeholder="$ts('location.placeholder')"
                :invalid="invalid"
                :described-by="describedBy"
                @update:model-value="field.handleChange($event)"
                @blur="field.handleBlur"
              />
            </template>
          </Field>
        </template>
      </form.Field>

      <div v-else class="space-y-1 text-sm" data-detected-place>
        <p class="flex items-center gap-1.5">
          <MapPin class="size-3.5 shrink-0 text-muted-foreground" />
          <span v-if="own?.place" class="font-medium">{{ own.place }}</span>
          <span v-else class="text-muted-foreground">{{ $t("location.notSet") }}</span>
        </p>
        <p class="text-xs text-muted-foreground">
          <template v-if="own?.detectedAt">
            {{ $t("location.detectedAt", { when: fully(own.detectedAt) }) }} ·
          </template>
          {{
            permission === "granted"
              ? $t("location.refreshes")
              : $t("location.refreshesWhenAllowed")
          }}
          <button
            type="button"
            class="ml-1 underline underline-offset-2 hover:text-foreground"
            :disabled="detecting"
            @click="detect"
          >
            {{ detecting ? $t("location.detecting") : $t("location.detectAgain") }}
          </button>
        </p>
      </div>

      <form.Field name="visibility">
        <template #default="{ field }">
          <VisibilityPicker
            :model-value="field.state.value"
            :legend="$ts('location.whoSees')"
            :hint="$ts('location.whoSeesHint')"
            @update:model-value="field.handleChange($event)"
          />
        </template>
      </form.Field>

      <Button type="submit">{{ $t("location.save") }}</Button>
    </form>

    <p class="mt-3 text-[0.6875rem] text-muted-foreground">{{ $t("location.privacyNote") }}</p>
  </section>
</template>
