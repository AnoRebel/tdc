<script setup lang="ts">
import { useForm } from "@tanstack/vue-form";
import * as v from "valibot";

import { isApiError, type EventKind, type ImportPlan, type Room } from "@tdc/types";
import { Button } from "#layers/ui/app/components/ui/button";
import { Checkbox } from "#layers/ui/app/components/ui/checkbox";
import { Field } from "#layers/ui/app/components/ui/field";
import { Select } from "#layers/ui/app/components/ui/select";
import { describeWhen, WHOLE_SERVER } from "#layers/ui/app/lib/events";

/**
 * Importing a calendar file: choose it, see what each event would become —
 * created, updated, or skipped because it is somebody else's — and the
 * warnings about anything that could not come across exactly, then import
 * the ones chosen.
 */
const emit = defineEmits<{ close: []; imported: [] }>();
const { $t, $ts } = useI18n();
const eventsApi = useEvents();
const roomsApi = useRooms();

const ics = ref("");
const fileName = ref("");
const plans = ref<ImportPlan[]>([]);
const chosen = ref<Set<string>>(new Set());
const failed = ref<string | null>(null);
const done = ref<string | null>(null);
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

async function choose(change: Event) {
  const file = (change.target as HTMLInputElement).files?.[0];
  if (!file) return;
  failed.value = null;
  done.value = null;
  if (file.size > 1024 * 1024) {
    failed.value = $ts("events.import.tooLarge");
    return;
  }
  fileName.value = file.name;
  ics.value = await file.text();
  try {
    plans.value = (await eventsApi.previewImport(ics.value)).events;
    chosen.value = new Set(
      plans.value.filter((plan) => plan.action !== "skip").map((plan) => plan.uid),
    );
  } catch (cause) {
    plans.value = [];
    failed.value = isApiError(cause) ? cause.message : $ts("events.import.couldNotRead");
  }
}

function toggle(uid: string, on: boolean) {
  const next = new Set(chosen.value);
  if (on) next.add(uid);
  else next.delete(uid);
  chosen.value = next;
}

const form = useForm({
  defaultValues: {
    channelId: WHOLE_SERVER,
    visibility: "public" as "public" | "channel" | "invite",
    kind: "other" as EventKind,
  },
  validators: {
    onSubmit: v.pipe(
      v.object({
        channelId: v.string(),
        visibility: v.picklist(["public", "channel", "invite"]),
        kind: v.picklist(["meetup", "talk", "workshop", "office_hours", "other"]),
      }),
      v.forward(
        v.check(
          (value) => value.visibility !== "channel" || value.channelId !== WHOLE_SERVER,
          $ts("events.form.channelRequired"),
        ),
        ["visibility"],
      ),
    ),
  },
  async onSubmit({ value }) {
    failed.value = null;
    try {
      const result = await eventsApi.import({
        ics: ics.value,
        uids: [...chosen.value],
        channelId: value.channelId === WHOLE_SERVER ? undefined : value.channelId,
        visibility: value.visibility,
        kind: value.kind,
      });
      done.value = $ts("events.import.done", {
        created: result.created,
        updated: result.updated,
        skipped: result.skipped,
      });
      emit("imported");
    } catch (cause) {
      failed.value = isApiError(cause) ? cause.message : $ts("events.import.couldNotImport");
    }
  },
});

const channelOptions = computed(() => [
  { value: WHOLE_SERVER, label: $ts("events.form.wholeServer") },
  ...channels.value.map((room) => ({ value: room.id, label: `#${room.name}` })),
]);
const visibilities = computed(() => [
  { value: "public" as const, label: $ts("events.visibility.public") },
  { value: "channel" as const, label: $ts("events.visibility.channel") },
  { value: "invite" as const, label: $ts("events.visibility.invite") },
]);
const kinds = computed(() =>
  (["meetup", "talk", "workshop", "office_hours", "other"] as const).map((kind) => ({
    value: kind,
    label: $ts(`events.kinds.${kind}`),
  })),
);

onKeyStroke("Escape", () => emit("close"));
</script>

<template>
  <Teleport to="body">
    <div
      class="fixed inset-0 z-50 flex items-start justify-center overflow-y-auto bg-black/50 p-4 backdrop-blur-sm md:py-10"
      role="dialog"
      aria-modal="true"
      :aria-label="$ts('events.import.title')"
      @click.self="emit('close')"
    >
      <div class="motion-pop-in w-full max-w-2xl rounded-2xl border bg-card shadow-2xl">
        <header class="flex items-center border-b px-5 py-3">
          <h2 class="text-sm font-semibold">{{ $t("events.import.title") }}</h2>
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
        <div class="space-y-4 px-5 py-4">
          <p class="text-xs text-muted-foreground">{{ $t("events.import.hint") }}</p>
          <label
            class="flex cursor-pointer items-center gap-3 rounded-xl border border-dashed px-4 py-5 hover:bg-accent/50"
          >
            <Icon name="lucide:file-up" class="size-5 text-muted-foreground" aria-hidden="true" />
            <span class="text-sm">{{ fileName || $t("events.import.choose") }}</span>
            <input type="file" accept=".ics,text/calendar" class="sr-only" @change="choose" />
          </label>

          <ul v-if="plans.length" class="max-h-72 divide-y overflow-y-auto rounded-xl border">
            <li v-for="plan in plans" :key="plan.uid" class="flex items-start gap-3 px-3 py-2.5">
              <Checkbox
                class="mt-0.5"
                :model-value="chosen.has(plan.uid)"
                :disabled="plan.action === 'skip'"
                :label="plan.title"
                @update:model-value="toggle(plan.uid, $event)"
              />
              <div class="min-w-0 flex-1">
                <p class="flex items-center gap-2 text-sm">
                  <span class="truncate font-medium">{{ plan.title }}</span>
                  <span
                    class="shrink-0 rounded px-1.5 py-0.5 text-[0.6875rem]"
                    :class="
                      plan.action === 'skip'
                        ? 'bg-muted text-muted-foreground'
                        : plan.action === 'update'
                          ? 'bg-warning/15 text-warning'
                          : 'bg-success/15 text-success'
                    "
                  >
                    {{ $t(`events.import.actions.${plan.action}`) }}
                  </span>
                </p>
                <p class="text-xs text-muted-foreground">
                  {{ describeWhen(plan, plan).day }} ·
                  {{ describeWhen(plan, plan).time || $t("events.allDay") }} · {{ plan.timeZone }}
                  <template v-if="plan.recurrence"> · {{ $t("events.repeats") }}</template>
                </p>
                <p v-if="plan.reason" class="text-xs text-muted-foreground">{{ plan.reason }}</p>
                <p v-for="warning in plan.warnings" :key="warning" class="text-xs text-warning">
                  {{ warning }}
                </p>
              </div>
            </li>
          </ul>

          <form
            v-if="plans.length"
            class="grid gap-3 sm:grid-cols-3"
            @submit.prevent="form.handleSubmit()"
          >
            <form.Field name="channelId">
              <template #default="{ field }">
                <Field :label="$ts('events.form.channel')">
                  <template #default="{ id }">
                    <Select
                      :id="id"
                      :model-value="field.state.value"
                      :label="$ts('events.form.channel')"
                      :options="channelOptions"
                      @update:model-value="field.handleChange($event ?? WHOLE_SERVER)"
                    />
                  </template>
                </Field>
              </template>
            </form.Field>
            <form.Field name="visibility">
              <template #default="{ field }">
                <Field
                  :label="$ts('events.form.visibility')"
                  :error="field.state.meta.errors[0]?.message"
                >
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
            <div class="flex items-center justify-end gap-2 sm:col-span-3">
              <p v-if="done" class="mr-auto text-xs text-success" role="status">{{ done }}</p>
              <Button variant="ghost" @click="emit('close')">{{ $t("action.close") }}</Button>
              <Button type="submit" :disabled="!chosen.size">{{
                $t("events.import.importChosen", { count: chosen.size })
              }}</Button>
            </div>
          </form>
          <p v-if="failed" class="text-sm text-destructive" role="alert">{{ failed }}</p>
        </div>
      </div>
    </div>
  </Teleport>
</template>
