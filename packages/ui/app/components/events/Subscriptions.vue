<script setup lang="ts">
import { useForm } from "@tanstack/vue-form";
import type { AnyFieldApi } from "@tanstack/vue-form";
import * as v from "valibot";

import { isApiError, type CalendarSubscription, type EventKind, type Room } from "@tdc/types";
import { Button } from "#layers/ui/app/components/ui/button";
import { Field } from "#layers/ui/app/components/ui/field";
import { Input } from "#layers/ui/app/components/ui/input";
import { Select } from "#layers/ui/app/components/ui/select";
import { fully } from "#layers/ui/app/lib/dates";
import { WHOLE_SERVER } from "#layers/ui/app/lib/events";

/**
 * Calendars published elsewhere that this server follows: a community's
 * Google Calendar, a conference's feed. Each is fetched on a schedule, and
 * its events appear here as events of their own, changed only there.
 */
const emit = defineEmits<{ close: [] }>();
const { $t, $ts } = useI18n();
const eventsApi = useEvents();
const roomsApi = useRooms();

const subscriptions = ref<CalendarSubscription[]>([]);
const channels = ref<Room[]>([]);
const refused = ref<string | null>(null);
const working = ref<string | null>(null);

async function load() {
  try {
    subscriptions.value = await eventsApi.subscriptions();
  } catch {
    subscriptions.value = [];
  }
}
onMounted(async () => {
  await load();
  try {
    channels.value = (await roomsApi.list()).filter(
      (room) => room.kind === "channel" && room.member,
    );
  } catch {
    channels.value = [];
  }
});

const form = useForm({
  defaultValues: {
    url: "",
    label: "",
    channelId: WHOLE_SERVER,
    visibility: "public" as "public" | "channel",
    kind: "other" as EventKind,
  },
  validators: {
    onSubmit: v.object({
      url: v.pipe(
        v.string(),
        v.trim(),
        v.regex(/^(https?|webcals?):\/\/\S+$/i, $ts("events.subscriptions.urlInvalid")),
      ),
      label: v.pipe(
        v.string(),
        v.trim(),
        v.minLength(1, $ts("events.subscriptions.labelRequired")),
        v.maxLength(80),
      ),
      channelId: v.string(),
      visibility: v.picklist(["public", "channel"]),
      kind: v.picklist(["meetup", "talk", "workshop", "office_hours", "other"]),
    }),
  },
  async onSubmit({ value }) {
    refused.value = null;
    try {
      const made = await eventsApi.subscribe({
        url: value.url.trim(),
        label: value.label.trim(),
        channelId: value.channelId === WHOLE_SERVER ? undefined : value.channelId,
        visibility: value.channelId === WHOLE_SERVER ? "public" : value.visibility,
        kind: value.kind,
      });
      if (made.lastError) refused.value = made.lastError;
      form.reset();
      await load();
    } catch (cause) {
      refused.value = isApiError(cause)
        ? cause.message
        : $ts("events.subscriptions.couldNotFollow");
    }
  },
});

function firstError(field: AnyFieldApi): string | undefined {
  const [issue] = field.state.meta.errors;
  if (!issue) return undefined;
  return typeof issue === "string" ? issue : (issue as { message?: string }).message;
}

async function refresh(subscription: CalendarSubscription) {
  working.value = subscription.id;
  try {
    await eventsApi.refreshSubscription(subscription.id);
    await load();
  } finally {
    working.value = null;
  }
}
async function remove(subscription: CalendarSubscription) {
  working.value = subscription.id;
  try {
    await eventsApi.unsubscribe(subscription.id);
    await load();
  } finally {
    working.value = null;
  }
}

const channelOptions = computed(() => [
  { value: WHOLE_SERVER, label: $ts("events.form.wholeServer") },
  ...channels.value.map((room) => ({ value: room.id, label: `#${room.name}` })),
]);
const visibilities = computed(() => [
  { value: "public" as const, label: $ts("events.visibility.public") },
  { value: "channel" as const, label: $ts("events.visibility.channel") },
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
      :aria-label="$ts('events.subscriptions.title')"
      @click.self="emit('close')"
    >
      <div class="motion-pop-in w-full max-w-2xl rounded-2xl border bg-card shadow-2xl">
        <header class="flex items-center border-b px-5 py-3">
          <h2 class="text-sm font-semibold">{{ $t("events.subscriptions.title") }}</h2>
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
          <p class="text-xs text-muted-foreground">{{ $t("events.subscriptions.hint") }}</p>

          <ul v-if="subscriptions.length" class="divide-y rounded-xl border">
            <li
              v-for="subscription in subscriptions"
              :key="subscription.id"
              class="flex items-start gap-3 px-3 py-2.5"
            >
              <Icon
                name="lucide:rss"
                class="mt-0.5 size-4 shrink-0 text-muted-foreground"
                aria-hidden="true"
              />
              <div class="min-w-0 flex-1">
                <p class="text-sm font-medium">{{ subscription.label }}</p>
                <p class="truncate text-xs text-muted-foreground">{{ subscription.url }}</p>
                <p
                  class="text-xs"
                  :class="subscription.lastError ? 'text-destructive' : 'text-muted-foreground'"
                >
                  <template v-if="subscription.paused"
                    >{{ $t("events.subscriptions.paused") }} ·
                  </template>
                  <template v-if="subscription.lastError">{{ subscription.lastError }}</template>
                  <template v-else-if="subscription.lastFetchedAt">{{
                    $t("events.subscriptions.fetched", { when: fully(subscription.lastFetchedAt) })
                  }}</template>
                </p>
              </div>
              <Button
                variant="ghost"
                size="small"
                :disabled="working === subscription.id"
                @click="refresh(subscription)"
              >
                {{ $t("events.subscriptions.refresh") }}
              </Button>
              <Button
                variant="danger"
                size="small"
                :disabled="working === subscription.id"
                @click="remove(subscription)"
              >
                {{ $t("events.subscriptions.remove") }}
              </Button>
            </li>
          </ul>

          <form class="space-y-3 rounded-xl border p-3" @submit.prevent="form.handleSubmit()">
            <h3 class="text-xs font-semibold">{{ $t("events.subscriptions.follow") }}</h3>
            <form.Field name="url">
              <template #default="{ field }">
                <Field
                  :label="$ts('events.subscriptions.url')"
                  :hint="$ts('events.subscriptions.urlHint')"
                  :error="firstError(field)"
                  required
                >
                  <template #default="{ id, invalid, describedBy }">
                    <Input
                      :id="id"
                      type="url"
                      placeholder="webcal://"
                      :model-value="field.state.value"
                      :invalid="invalid"
                      :described-by="describedBy"
                      @update:model-value="field.handleChange"
                    />
                  </template>
                </Field>
              </template>
            </form.Field>
            <div class="grid gap-3 sm:grid-cols-2">
              <form.Field name="label">
                <template #default="{ field }">
                  <Field
                    :label="$ts('events.subscriptions.label')"
                    :error="firstError(field)"
                    required
                  >
                    <template #default="{ id, invalid }">
                      <Input
                        :id="id"
                        :model-value="field.state.value"
                        :invalid="invalid"
                        @update:model-value="field.handleChange"
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
                  <Field :label="$ts('events.form.visibility')">
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
            <p v-if="refused" class="text-xs text-destructive" role="alert">{{ refused }}</p>
            <div class="flex justify-end">
              <Button type="submit">{{ $t("events.subscriptions.follow") }}</Button>
            </div>
          </form>
        </div>
      </div>
    </div>
  </Teleport>
</template>
