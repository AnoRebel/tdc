<script setup lang="ts">
import { useForm } from "@tanstack/vue-form";
import type { AnyFieldApi } from "@tanstack/vue-form";
import * as v from "valibot";

import {
  isApiError,
  type EventDetail,
  type EventInput,
  type EventKind,
  type Room,
} from "@tdc/types";
import { Button } from "#layers/ui/app/components/ui/button";
import { Field } from "#layers/ui/app/components/ui/field";
import { Input } from "#layers/ui/app/components/ui/input";
import { Select } from "#layers/ui/app/components/ui/select";
import { Switch } from "#layers/ui/app/components/ui/switch";
import { Textarea } from "#layers/ui/app/components/ui/textarea";
import {
  buildRule,
  fromLocalInput,
  parseRule,
  toDateInput,
  toLocalInput,
  viewerZone,
  weekdays,
  zoneNames,
  WHOLE_SERVER,
  type Picked,
  type Repeat,
} from "#layers/ui/app/lib/events";

/**
 * Scheduling an event, or changing one.
 *
 * Times are entered as the organiser's wall clock in the event's zone, which
 * is the zone the form opens in and the one the times are read in when it is
 * saved — so choosing London and typing 19:00 means 19:00 in London, wherever
 * the person filling it in happens to be.
 */
const props = defineProps<{ event?: EventDetail; channelId?: string }>();
const emit = defineEmits<{ saved: [event: EventDetail]; close: [] }>();

const { $t, $ts } = useI18n();
const eventsApi = useEvents();
const roomsApi = useRooms();

const channels = ref<Room[]>([]);
onMounted(async () => {
  try {
    channels.value = (await roomsApi.list()).filter(
      (room) => room.kind === "channel" && room.member,
    );
  } catch {
    channels.value = [];
  }
});

const existing = props.event;
const startingZone = existing?.timeZone ?? viewerZone();
const inAnHour = new Date(Date.now() + 60 * 60 * 1000);
inAnHour.setMinutes(0, 0, 0);
const defaultStart = existing?.startsAt ?? inAnHour.toISOString();
const defaultEnd =
  existing?.endsAt ?? new Date(inAnHour.getTime() + 2 * 60 * 60 * 1000).toISOString();

/*
 * The rules an event must meet, stated once. Whether a channel may hold a
 * public event, and whether a repeat is one events support, are the server's
 * to say: it knows the channel, and it is the one that expands the repeat.
 */
const schema = v.pipe(
  v.object({
    title: v.pipe(
      v.string(),
      v.trim(),
      v.minLength(1, $ts("events.form.titleRequired")),
      v.maxLength(200),
    ),
    kind: v.picklist(["meetup", "talk", "workshop", "office_hours", "other"]),
    description: v.pipe(v.string(), v.maxLength(20000)),
    timeZone: v.pipe(v.string(), v.minLength(1, $ts("events.form.zoneRequired"))),
    allDay: v.boolean(),
    start: v.pipe(v.string(), v.minLength(1, $ts("events.form.startRequired"))),
    end: v.pipe(v.string(), v.minLength(1, $ts("events.form.endRequired"))),
    channelId: v.string(),
    visibility: v.picklist(["public", "channel", "invite"]),
    location: v.pipe(v.string(), v.maxLength(500)),
    online: v.boolean(),
    onlineUrl: v.union([v.literal(""), v.pipe(v.string(), v.url($ts("events.form.urlInvalid")))]),
    capacity: v.union([
      v.literal(""),
      v.pipe(v.string(), v.regex(/^[1-9][0-9]{0,5}$/, $ts("events.form.capacityInvalid"))),
    ]),
  }),
  v.forward(
    v.check((value) => value.end >= value.start, $ts("events.form.endBeforeStart")),
    ["end"],
  ),
  v.forward(
    v.check(
      (value) => value.visibility !== "channel" || value.channelId !== WHOLE_SERVER,
      $ts("events.form.channelRequired"),
    ),
    ["visibility"],
  ),
);

const repeat = ref<Repeat>(parseRule(existing?.recurrence, startingZone));
const hosts = ref<Picked[]>(
  (existing?.hosts ?? [])
    .filter((person) => person.userId !== existing?.createdBy?.userId)
    .map((person) => ({ id: person.userId, name: person.username, kind: "person" as const })),
);
const speakers = ref<Picked[]>(
  (existing?.speakers ?? []).map((person) => ({
    id: person.userId,
    name: person.username,
    kind: "person" as const,
  })),
);

interface SessionRow {
  key: string;
  title: string;
  start: string;
  end: string;
  speakers: Picked[];
  guestSpeakers: string;
}
const sessions = ref<SessionRow[]>(
  (existing?.sessions ?? []).map((session) => ({
    key: session.id,
    title: session.title,
    start: toLocalInput(session.startsAt, startingZone),
    end: toLocalInput(session.endsAt, startingZone),
    speakers: session.speakers.map((person) => ({
      id: person.userId,
      name: person.username,
      kind: "person" as const,
    })),
    guestSpeakers: session.guestSpeakers ?? "",
  })),
);

const refused = ref<string | null>(null);

const form = useForm({
  defaultValues: {
    title: existing?.title ?? "",
    kind: (existing?.kind ?? "meetup") as EventKind,
    description: existing?.description ?? "",
    timeZone: startingZone,
    allDay: existing?.allDay ?? false,
    start: existing?.allDay
      ? toDateInput(defaultStart, startingZone)
      : toLocalInput(defaultStart, startingZone),
    end: existing?.allDay
      ? toDateInput(new Date(new Date(defaultEnd).getTime() - 1), startingZone)
      : toLocalInput(defaultEnd, startingZone),
    channelId: existing?.channel?.id ?? props.channelId ?? WHOLE_SERVER,
    visibility: (existing?.visibility ?? (props.channelId ? "channel" : "public")) as
      "public" | "channel" | "invite",
    location: existing?.location ?? "",
    online: existing?.online ?? false,
    onlineUrl: existing?.onlineUrl ?? "",
    capacity: existing?.capacity ? String(existing.capacity) : "",
  },
  validators: { onSubmit: schema },
  async onSubmit({ value }) {
    refused.value = null;
    const zone = value.timeZone;
    const startsAt = value.allDay
      ? fromLocalInput(`${value.start.slice(0, 10)}T00:00`, zone)
      : fromLocalInput(value.start, zone);
    // An all-day event ends at the midnight after its last day.
    const endsAt = value.allDay
      ? new Date(
          new Date(fromLocalInput(`${value.end.slice(0, 10)}T00:00`, zone)).getTime() + 86_400_000,
        ).toISOString()
      : fromLocalInput(value.end, zone);
    const input: EventInput = {
      channelId: value.channelId === WHOLE_SERVER ? undefined : value.channelId,
      kind: value.kind,
      title: value.title.trim(),
      description: value.description,
      startsAt,
      endsAt,
      timeZone: zone,
      allDay: value.allDay,
      recurrence: buildRule(
        repeat.value,
        value.allDay ? `${value.start.slice(0, 10)}T00:00` : value.start,
        zone,
      ),
      exceptions: existing?.exceptions ?? [],
      location: value.location.trim(),
      online: value.online,
      onlineUrl: value.onlineUrl.trim(),
      capacity: value.capacity ? Number(value.capacity) : null,
      visibility: value.visibility,
      hosts: hosts.value.map((each) => each.id),
      speakers: speakers.value.map((each) => each.id),
      sessions: sessions.value
        .filter((session) => session.title.trim())
        .map((session) => ({
          title: session.title.trim(),
          startsAt: fromLocalInput(session.start, zone),
          endsAt: fromLocalInput(session.end, zone),
          speakers: session.speakers.map((each) => each.id),
          guestSpeakers: session.guestSpeakers.trim(),
        })),
    };
    try {
      const saved = existing
        ? await eventsApi.update(existing.id, input)
        : await eventsApi.create(input);
      emit("saved", saved);
    } catch (cause) {
      refused.value = isApiError(cause) ? cause.message : $ts("events.form.couldNotSave");
    }
  },
});

function firstError(field: AnyFieldApi): string | undefined {
  if (!field.state.meta.isTouched && !form.state.submissionAttempts) return undefined;
  const [issue] = field.state.meta.errors;
  if (!issue) return undefined;
  return typeof issue === "string" ? issue : (issue as { message?: string }).message;
}

const kinds = computed(() =>
  (["meetup", "talk", "workshop", "office_hours", "other"] as const).map((kind) => ({
    value: kind,
    label: $ts(`events.kinds.${kind}`),
  })),
);
const channelOptions = computed(() => [
  { value: WHOLE_SERVER, label: $ts("events.form.wholeServer") },
  ...channels.value.map((room) => ({ value: room.id, label: `#${room.name}` })),
]);
const visibilities = computed(() => [
  { value: "public" as const, label: $ts("events.visibility.public") },
  { value: "channel" as const, label: $ts("events.visibility.channel") },
  { value: "invite" as const, label: $ts("events.visibility.invite") },
]);
const frequencies = computed(() => [
  { value: "none" as const, label: $ts("events.repeat.none") },
  { value: "daily" as const, label: $ts("events.repeat.daily") },
  { value: "weekly" as const, label: $ts("events.repeat.weekly") },
  { value: "monthly" as const, label: $ts("events.repeat.monthly") },
]);
const endings = computed(() => [
  { value: "never" as const, label: $ts("events.repeat.never") },
  { value: "count" as const, label: $ts("events.repeat.afterCount") },
  { value: "until" as const, label: $ts("events.repeat.onDate") },
]);
const monthlyModes = computed(() => [
  { value: "date" as const, label: $ts("events.repeat.onTheDate") },
  { value: "weekday" as const, label: $ts("events.repeat.onTheWeekday") },
]);
const zones = zoneNames();
const zonesId = useId();

function toggleDay(day: (typeof weekdays)[number]) {
  const days = repeat.value.days.includes(day)
    ? repeat.value.days.filter((each) => each !== day)
    : [...repeat.value.days, day];
  repeat.value = { ...repeat.value, days };
}

function addSession() {
  const start = form.state.values.start;
  sessions.value = [
    ...sessions.value,
    { key: crypto.randomUUID(), title: "", start, end: start, speakers: [], guestSpeakers: "" },
  ];
}

const dialog = ref<HTMLElement | null>(null);
onMounted(() => {
  document.body.style.overflow = "hidden";
  nextTick(() => dialog.value?.querySelector<HTMLInputElement>("input")?.focus());
});
onBeforeUnmount(() => {
  document.body.style.overflow = "";
});
onKeyStroke("Escape", () => emit("close"));
</script>

<template>
  <Teleport to="body">
    <div
      class="fixed inset-0 z-50 flex items-start justify-center overflow-y-auto bg-black/50 p-4 backdrop-blur-sm md:py-10"
      role="dialog"
      aria-modal="true"
      :aria-label="existing ? $ts('events.form.editTitle') : $ts('events.form.newTitle')"
      @click.self="emit('close')"
    >
      <div
        ref="dialog"
        class="motion-pop-in w-full max-w-2xl rounded-2xl border bg-card shadow-2xl"
      >
        <header class="flex items-center gap-3 border-b px-5 py-3">
          <h2 class="text-sm font-semibold">
            {{ existing ? $t("events.form.editTitle") : $t("events.form.newTitle") }}
          </h2>
          <Button
            variant="ghost"
            size="icon"
            class="ml-auto"
            :label="$ts('action.close')"
            @click="emit('close')"
          >
            <Icon name="lucide:x" class="size-4" aria-hidden="true" />
          </Button>
        </header>

        <form class="space-y-5 px-5 py-4" novalidate @submit.prevent="form.handleSubmit()">
          <div class="grid gap-3 sm:grid-cols-[1fr_12rem]">
            <form.Field name="title">
              <template #default="{ field }">
                <Field :label="$ts('events.form.title')" :error="firstError(field)" required>
                  <template #default="{ id, invalid, describedBy }">
                    <Input
                      :id="id"
                      :model-value="field.state.value"
                      :invalid="invalid"
                      :described-by="describedBy"
                      :placeholder="$ts('events.form.titlePlaceholder')"
                      @update:model-value="field.handleChange"
                      @blur="field.handleBlur"
                    />
                  </template>
                </Field>
              </template>
            </form.Field>
            <form.Field name="kind">
              <template #default="{ field }">
                <Field :label="$ts('events.form.kind')">
                  <template #default="{ id }">
                    <Select
                      :id="id"
                      :model-value="field.state.value"
                      :label="$ts('events.form.kind')"
                      :options="kinds"
                      @update:model-value="$event && field.handleChange($event)"
                    />
                  </template>
                </Field>
              </template>
            </form.Field>
          </div>

          <form.Field name="description">
            <template #default="{ field }">
              <Field
                :label="$ts('events.form.description')"
                :hint="$ts('events.form.descriptionHint')"
              >
                <template #default="{ id, describedBy }">
                  <Textarea
                    :id="id"
                    rows="4"
                    :model-value="field.state.value"
                    :described-by="describedBy"
                    @update:model-value="field.handleChange"
                  />
                </template>
              </Field>
            </template>
          </form.Field>

          <!-- When -->
          <fieldset class="space-y-3 rounded-xl border p-3">
            <legend class="px-1 text-xs font-semibold">{{ $t("events.form.when") }}</legend>
            <form.Field name="allDay">
              <template #default="{ field }">
                <div class="flex items-center gap-2 text-sm">
                  <Switch
                    :model-value="field.state.value"
                    :label="$ts('events.allDay')"
                    @update:model-value="field.handleChange"
                  />
                  {{ $t("events.allDay") }}
                </div>
              </template>
            </form.Field>
            <form.Subscribe v-slot="{ values }">
              <div class="grid gap-3 sm:grid-cols-2">
                <form.Field name="start">
                  <template #default="{ field }">
                    <Field :label="$ts('events.form.starts')" :error="firstError(field)" required>
                      <template #default="{ id, invalid, describedBy }">
                        <Input
                          :id="id"
                          :type="values.allDay ? 'date' : 'datetime-local'"
                          :model-value="
                            values.allDay ? field.state.value.slice(0, 10) : field.state.value
                          "
                          :invalid="invalid"
                          :described-by="describedBy"
                          @update:model-value="field.handleChange"
                          @blur="field.handleBlur"
                        />
                      </template>
                    </Field>
                  </template>
                </form.Field>
                <form.Field name="end">
                  <template #default="{ field }">
                    <Field :label="$ts('events.form.ends')" :error="firstError(field)" required>
                      <template #default="{ id, invalid, describedBy }">
                        <Input
                          :id="id"
                          :type="values.allDay ? 'date' : 'datetime-local'"
                          :model-value="
                            values.allDay ? field.state.value.slice(0, 10) : field.state.value
                          "
                          :invalid="invalid"
                          :described-by="describedBy"
                          @update:model-value="field.handleChange"
                          @blur="field.handleBlur"
                        />
                      </template>
                    </Field>
                  </template>
                </form.Field>
              </div>
            </form.Subscribe>
            <form.Field name="timeZone">
              <template #default="{ field }">
                <Field
                  :label="$ts('events.form.timeZone')"
                  :hint="$ts('events.form.timeZoneHint')"
                  :error="firstError(field)"
                >
                  <template #default="{ id, invalid, describedBy }">
                    <Input
                      :id="id"
                      :model-value="field.state.value"
                      :invalid="invalid"
                      :described-by="describedBy"
                      :list="zonesId"
                      autocomplete="off"
                      @update:model-value="field.handleChange"
                    />
                    <datalist :id="zonesId">
                      <option v-for="zone in zones" :key="zone" :value="zone" />
                    </datalist>
                  </template>
                </Field>
              </template>
            </form.Field>

            <div class="grid gap-3 sm:grid-cols-2">
              <Field :label="$ts('events.form.repeats')">
                <template #default="{ id }">
                  <Select
                    :id="id"
                    v-model="repeat.frequency"
                    :label="$ts('events.form.repeats')"
                    :options="frequencies"
                  />
                </template>
              </Field>
              <Field v-if="repeat.frequency !== 'none'" :label="$ts('events.repeat.every')">
                <template #default="{ id }">
                  <Input
                    :id="id"
                    type="number"
                    :model-value="String(repeat.interval)"
                    @update:model-value="repeat.interval = Number($event) || 1"
                  />
                </template>
              </Field>
            </div>
            <fieldset v-if="repeat.frequency === 'weekly'" class="flex flex-wrap gap-1.5">
              <legend class="mb-1 text-xs font-medium">{{ $t("events.repeat.onDays") }}</legend>
              <button
                v-for="day in weekdays"
                :key="day"
                type="button"
                class="h-7 min-w-9 cursor-pointer rounded-md border px-2 text-xs transition-colors"
                :class="
                  repeat.days.includes(day)
                    ? 'border-primary bg-primary text-primary-foreground'
                    : 'hover:bg-accent'
                "
                :aria-pressed="repeat.days.includes(day)"
                @click="toggleDay(day)"
              >
                {{ $t(`events.weekdays.${day}`) }}
              </button>
            </fieldset>
            <Field v-if="repeat.frequency === 'monthly'" :label="$ts('events.repeat.monthlyOn')">
              <template #default="{ id }">
                <Select
                  :id="id"
                  v-model="repeat.monthly"
                  :label="$ts('events.repeat.monthlyOn')"
                  :options="monthlyModes"
                />
              </template>
            </Field>
            <div v-if="repeat.frequency !== 'none'" class="grid gap-3 sm:grid-cols-2">
              <Field :label="$ts('events.repeat.ends')">
                <template #default="{ id }">
                  <Select
                    :id="id"
                    v-model="repeat.end"
                    :label="$ts('events.repeat.ends')"
                    :options="endings"
                  />
                </template>
              </Field>
              <Field v-if="repeat.end === 'count'" :label="$ts('events.repeat.times')">
                <template #default="{ id }">
                  <Input
                    :id="id"
                    type="number"
                    :model-value="String(repeat.count)"
                    @update:model-value="repeat.count = Number($event) || 1"
                  />
                </template>
              </Field>
              <Field v-if="repeat.end === 'until'" :label="$ts('events.repeat.lastDay')">
                <template #default="{ id }">
                  <Input :id="id" v-model="repeat.until" type="date" />
                </template>
              </Field>
            </div>
          </fieldset>

          <!-- Where -->
          <fieldset class="space-y-3 rounded-xl border p-3">
            <legend class="px-1 text-xs font-semibold">{{ $t("events.form.where") }}</legend>
            <form.Field name="location">
              <template #default="{ field }">
                <Field :label="$ts('events.form.location')">
                  <template #default="{ id }">
                    <Input
                      :id="id"
                      :model-value="field.state.value"
                      :placeholder="$ts('events.form.locationPlaceholder')"
                      @update:model-value="field.handleChange"
                    />
                  </template>
                </Field>
              </template>
            </form.Field>
            <form.Field name="online">
              <template #default="{ field }">
                <div class="flex items-start gap-2 text-sm">
                  <Switch
                    :model-value="field.state.value"
                    :label="$ts('events.form.hostHere')"
                    class="mt-0.5"
                    @update:model-value="field.handleChange"
                  />
                  <span>
                    <span class="block">{{ $t("events.form.hostHere") }}</span>
                    <span class="block text-xs text-muted-foreground">{{
                      $t("events.form.hostHereHint")
                    }}</span>
                  </span>
                </div>
              </template>
            </form.Field>
            <form.Field name="onlineUrl">
              <template #default="{ field }">
                <Field
                  :label="$ts('events.form.onlineUrl')"
                  :hint="$ts('events.form.onlineUrlHint')"
                  :error="firstError(field)"
                >
                  <template #default="{ id, invalid, describedBy }">
                    <Input
                      :id="id"
                      type="url"
                      :model-value="field.state.value"
                      :invalid="invalid"
                      :described-by="describedBy"
                      placeholder="https://"
                      @update:model-value="field.handleChange"
                      @blur="field.handleBlur"
                    />
                  </template>
                </Field>
              </template>
            </form.Field>
          </fieldset>

          <!-- Who -->
          <fieldset class="space-y-3 rounded-xl border p-3">
            <legend class="px-1 text-xs font-semibold">{{ $t("events.form.who") }}</legend>
            <div class="grid gap-3 sm:grid-cols-2">
              <form.Field name="channelId">
                <template #default="{ field }">
                  <Field :label="$ts('events.form.channel')">
                    <template #default="{ id }">
                      <Select
                        :id="id"
                        :model-value="field.state.value"
                        :label="$ts('events.form.channel')"
                        :options="channelOptions"
                        :disabled="Boolean(existing)"
                        @update:model-value="field.handleChange($event ?? WHOLE_SERVER)"
                      />
                    </template>
                  </Field>
                </template>
              </form.Field>
              <form.Field name="visibility">
                <template #default="{ field }">
                  <Field :label="$ts('events.form.visibility')" :error="firstError(field)">
                    <template #default="{ id }">
                      <Select
                        :id="id"
                        :model-value="field.state.value"
                        :label="$ts('events.form.visibility')"
                        :options="visibilities"
                        @update:model-value="$event && field.handleChange($event)"
                      />
                    </template>
                  </Field>
                </template>
              </form.Field>
            </div>
            <form.Field name="capacity">
              <template #default="{ field }">
                <Field
                  :label="$ts('events.form.capacity')"
                  :hint="$ts('events.form.capacityHint')"
                  :error="firstError(field)"
                >
                  <template #default="{ id, invalid, describedBy }">
                    <Input
                      :id="id"
                      type="number"
                      class="max-w-40"
                      :model-value="field.state.value"
                      :invalid="invalid"
                      :described-by="describedBy"
                      @update:model-value="(value: string | number) => field.handleChange(String(value ?? ''))"
                      @blur="field.handleBlur"
                    />
                  </template>
                </Field>
              </template>
            </form.Field>
            <EventsPeoplePicker v-model="hosts" :label="$ts('events.form.hosts')" />
            <EventsPeoplePicker v-model="speakers" :label="$ts('events.form.speakers')" />
          </fieldset>

          <!-- Agenda -->
          <fieldset class="space-y-3 rounded-xl border p-3">
            <legend class="px-1 text-xs font-semibold">{{ $t("events.agenda") }}</legend>
            <p class="text-xs text-muted-foreground">{{ $t("events.form.agendaHint") }}</p>
            <div
              v-for="(session, index) in sessions"
              :key="session.key"
              class="space-y-2 rounded-lg bg-muted/30 p-2.5"
            >
              <div class="flex items-end gap-2">
                <Field :label="$ts('events.form.sessionTitle')" class="flex-1">
                  <template #default="{ id }">
                    <Input :id="id" v-model="session.title" />
                  </template>
                </Field>
                <Button
                  variant="ghost"
                  size="icon"
                  :label="$ts('events.form.removeSession')"
                  @click="sessions.splice(index, 1)"
                >
                  <Icon name="lucide:trash-2" class="size-3.5" aria-hidden="true" />
                </Button>
              </div>
              <div class="grid gap-2 sm:grid-cols-2">
                <Field :label="$ts('events.form.starts')">
                  <template #default="{ id }">
                    <Input :id="id" v-model="session.start" type="datetime-local" />
                  </template>
                </Field>
                <Field :label="$ts('events.form.ends')">
                  <template #default="{ id }">
                    <Input :id="id" v-model="session.end" type="datetime-local" />
                  </template>
                </Field>
              </div>
              <EventsPeoplePicker v-model="session.speakers" :label="$ts('events.form.speakers')" />
              <Field
                :label="$ts('events.form.guestSpeakers')"
                :hint="$ts('events.form.guestSpeakersHint')"
              >
                <template #default="{ id, describedBy }">
                  <Input :id="id" v-model="session.guestSpeakers" :described-by="describedBy" />
                </template>
              </Field>
            </div>
            <Button variant="outline" @click="addSession">
              <Icon name="lucide:plus" class="size-3.5" aria-hidden="true" />
              {{ $t("events.form.addSession") }}
            </Button>
          </fieldset>

          <p v-if="refused" class="text-sm text-destructive" role="alert">{{ refused }}</p>

          <div class="flex justify-end gap-2 border-t pt-4">
            <Button variant="ghost" @click="emit('close')">{{ $t("action.cancel") }}</Button>
            <form.Subscribe v-slot="{ isSubmitting }">
              <Button type="submit" :disabled="isSubmitting">
                {{ existing ? $t("action.save") : $t("events.form.schedule") }}
              </Button>
            </form.Subscribe>
          </div>
        </form>
      </div>
    </div>
  </Teleport>
</template>
