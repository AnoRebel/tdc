<script setup lang="ts">
import { nameOf } from "#layers/ui/app/lib/names";
import { isApiError, type Call, type EventAnswer, type EventDetail } from "@tdc/types";
import { Button } from "#layers/ui/app/components/ui/button";
import { Avatar } from "#layers/ui/app/components/ui/avatar";
import { Input } from "#layers/ui/app/components/ui/input";
import { Switch } from "#layers/ui/app/components/ui/switch";
import { googleCalendarLink, outlookLink, parseRule } from "#layers/ui/app/lib/events";

/**
 * One event: when in both zones, where, its call, its agenda, who is running
 * it and who is going, the person's own answer, and the ways to put it in
 * another calendar.
 */
definePageMeta({ middleware: "auth", layout: "app" });

const { $t, $ts, $localeRoute: localeRoute } = useI18n();
const route = useRoute();
const router = useRouter();
const eventsApi = useEvents();
const api = useApi();
const download = useDownload();
const mediaUrl = useMediaUrl();
const serverBase = useMediaBase();
const { offers } = useServerInfo();
const calling = useCall();
const uploads = useUploads();

const id = computed(() => String(route.params.id));
const event = ref<EventDetail | null>(null);
const failed = ref<string | null>(null);
const busy = ref(false);
const editing = ref(false);
const cancelling = ref(false);
const deleting = ref(false);
const refusal = ref<string | null>(null);

async function load() {
  failed.value = null;
  try {
    event.value = await eventsApi.get(id.value);
    useHead({ title: event.value.title });
  } catch (cause) {
    failed.value =
      isApiError(cause) && cause.status === 404
        ? $ts("events.notFound")
        : $ts("events.couldNotLoad");
  }
}
watch(id, () => void load(), { immediate: true });

const occurrence = computed(() =>
  event.value
    ? (event.value.next ?? { startsAt: event.value.startsAt, endsAt: event.value.endsAt })
    : null,
);
const cancelled = computed(() => Boolean(event.value?.cancelledAt));
const ended = computed(() => Boolean(event.value && !event.value.next && !cancelled.value));
const placesLeft = computed(() =>
  event.value?.capacity ? Math.max(0, event.value.capacity - event.value.counts.going) : null,
);

/* Answering */
async function answer(status: Exclude<EventAnswer, "waitlisted">) {
  if (!event.value) return;
  busy.value = true;
  refusal.value = null;
  try {
    await eventsApi.answer(event.value.id, status);
    await load();
    attendees.value?.refresh();
  } catch (cause) {
    refusal.value = isApiError(cause) ? cause.message : $ts("events.couldNotAnswer");
  } finally {
    busy.value = false;
  }
}
const attendees = ref<{ refresh: () => void } | null>(null);

/* The call */
const roomCall = ref<Call | null>(null);
const callsOffered = computed(() => offers("calls") && calling.supported.value);
async function refreshCall() {
  if (!event.value?.online || !event.value.member || !callsOffered.value) return;
  try {
    roomCall.value = await api.get<Call>(`/api/rooms/${event.value.id}/call`);
  } catch {
    roomCall.value = null;
  }
}
watch(event, () => void refreshCall());
const timer = setInterval(() => void refreshCall(), 15_000);
onBeforeUnmount(() => clearInterval(timer));
const inCall = computed(() => Boolean(event.value && calling.isIn(event.value.id)));
async function startCall(video: boolean) {
  if (!event.value) return;
  await calling.join(event.value.id, video);
  await refreshCall();
}

/* Repeats, described */
const repeatText = computed(() => {
  if (!event.value?.recurrence) return "";
  const repeat = parseRule(event.value.recurrence, event.value.timeZone);
  // "Weekly" rather than "every 1 week".
  const every =
    repeat.interval > 1
      ? $ts(`events.repeat.summary.${repeat.frequency}`, { count: repeat.interval })
      : $ts(`events.repeat.${repeat.frequency}`);
  if (repeat.end === "count")
    return `${every} · ${$ts("events.repeat.summary.times", { count: repeat.count })}`;
  if (repeat.end === "until")
    return `${every} · ${$ts("events.repeat.summary.until", { day: repeat.until })}`;
  return every;
});

/* Other calendars */
// The event's address on the server, which on the desktop is not this page's.
const details = computed(() =>
  `${event.value?.description ?? ""}\n\n${serverBase || window.location.origin}${route.fullPath}`.trim(),
);
const googleHref = computed(() =>
  event.value && occurrence.value
    ? googleCalendarLink(event.value, occurrence.value, details.value)
    : "",
);
const outlookHref = computed(() =>
  event.value && occurrence.value ? outlookLink(event.value, occurrence.value, details.value) : "",
);
async function downloadICS() {
  if (event.value) await download(`/api/events/${event.value.id}/ics`, "event.ics");
}

/* Changing it */
async function saved(changed: EventDetail) {
  editing.value = false;
  event.value = changed;
}
const cancelReason = ref("");
async function confirmCancel() {
  if (!event.value) return;
  try {
    event.value = await eventsApi.cancel(event.value.id, cancelReason.value);
  } catch (cause) {
    refusal.value = isApiError(cause) ? cause.message : $ts("events.couldNotSave");
  } finally {
    cancelling.value = false;
  }
}
async function confirmDelete() {
  if (!event.value) return;
  deleting.value = false;
  try {
    await eventsApi.remove(event.value.id);
    await router.push(localeRoute({ name: "events" }));
  } catch (cause) {
    refusal.value = isApiError(cause) ? cause.message : $ts("events.couldNotSave");
  }
}

/* Reminders, which muting the event's conversation silences. */
async function setMuted(muted: boolean) {
  if (!event.value) return;
  try {
    await api.put("/api/me/preferences/notifications", {
      room: event.value.id,
      level: "mentions",
      muted,
    });
    event.value = { ...event.value, muted };
  } catch (cause) {
    refusal.value = isApiError(cause) ? cause.message : $ts("events.couldNotSave");
  }
}

/* The cover */
const coverInput = ref<HTMLInputElement | null>(null);
const uploading = ref(false);
async function chooseCover(change: Event) {
  const file = (change.target as HTMLInputElement).files?.[0];
  if (!file || !event.value) return;
  uploading.value = true;
  refusal.value = null;
  try {
    const done = await uploads.upload(event.value.id, file);
    if (!done.attachment) throw new Error(done.error ?? "");
    event.value = await eventsApi.setCover(event.value.id, done.attachment.id);
  } catch (cause) {
    refusal.value = isApiError(cause) ? cause.message : $ts("events.couldNotUploadCover");
  } finally {
    uploading.value = false;
  }
}

onKeyStroke("Escape", () => {
  cancelling.value = false;
});

const answers = computed(() => [
  { value: "going" as const, label: $ts("events.answers.going"), icon: "check" },
  { value: "maybe" as const, label: $ts("events.answers.maybe"), icon: "help" },
  { value: "declined" as const, label: $ts("events.answers.declined"), icon: "x" },
]);
</script>

<template>
  <div class="flex-1 overflow-y-auto">
    <header class="flex h-12 shrink-0 items-center gap-2 border-b px-4">
      <NuxtLink
        :to="localeRoute({ name: 'events' })"
        class="flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
      >
        <Icon name="lucide:arrow-left" class="size-3.5" aria-hidden="true" />
        {{ $t("events.title") }}
      </NuxtLink>
      <div v-if="event" class="ml-auto flex items-center gap-1">
        <Button
          v-if="event.may.edit && !event.imported && !cancelled"
          variant="ghost"
          size="small"
          @click="editing = true"
        >
          <Icon name="lucide:pencil" class="size-3.5" aria-hidden="true" />
          {{ $t("action.edit") }}
        </Button>
        <Button
          v-if="event.may.edit && !cancelled"
          variant="ghost"
          size="small"
          @click="cancelling = true"
        >
          {{ $t("events.cancelEvent") }}
        </Button>
        <Button v-if="event.may.delete" variant="danger" size="small" @click="deleting = true">
          {{ $t("action.delete") }}
        </Button>
      </div>
    </header>

    <p v-if="failed" class="px-4 py-10 text-center text-sm text-muted-foreground" role="alert">
      {{ failed }}
    </p>
    <div v-else-if="!event" class="mx-auto max-w-3xl space-y-3 px-4 py-6" aria-busy="true">
      <UiSkeleton class="h-8 w-2/3" />
      <UiSkeleton class="h-24 w-full" />
    </div>

    <article v-else class="mx-auto max-w-4xl px-4 py-6">
      <div v-if="event.cover" class="mb-5 aspect-[3/1] overflow-hidden rounded-2xl border bg-muted">
        <img
          :src="mediaUrl(event.cover.previewUrl || event.cover.url)"
          alt=""
          class="size-full object-cover"
        />
      </div>

      <div
        v-if="cancelled"
        class="mb-4 flex items-start gap-2 rounded-xl border border-destructive/30 bg-destructive/5 px-3 py-2.5 text-sm"
        role="status"
      >
        <Icon
          name="lucide:calendar-x"
          class="mt-0.5 size-4 shrink-0 text-destructive"
          aria-hidden="true"
        />
        <span>
          <span class="font-medium">{{ $t("events.cancelledBanner") }}</span>
          <span v-if="event.cancelReason" class="block text-muted-foreground">{{
            event.cancelReason
          }}</span>
        </span>
      </div>

      <div class="grid gap-8 md:grid-cols-[1fr_17rem]">
        <div class="min-w-0 space-y-6">
          <div>
            <p class="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
              <span class="rounded bg-muted px-1.5 py-0.5">{{
                $t(`events.kinds.${event.kind}`)
              }}</span>
              <NuxtLink
                v-if="event.channel"
                :to="`/rooms/${event.channel.id}`"
                class="inline-flex items-center gap-0.5 hover:text-foreground"
              >
                <Icon name="lucide:hash" class="size-3" aria-hidden="true" />{{
                  event.channel.name
                }}
              </NuxtLink>
              <span v-else>{{ $t("events.wholeServer") }}</span>
              <span>· {{ $t(`events.visibility.${event.visibility}`) }}</span>
              <span v-if="event.imported">· {{ $t("events.imported") }}</span>
            </p>
            <h1
              class="mt-2 text-2xl font-semibold tracking-tight text-balance"
              :class="cancelled && 'line-through decoration-muted-foreground/50'"
            >
              {{ event.title }}
            </h1>
          </div>

          <dl class="grid gap-4 sm:grid-cols-2">
            <div class="flex gap-3">
              <dt class="sr-only">{{ $t("events.form.when") }}</dt>
              <Icon
                name="lucide:calendar-clock"
                class="mt-0.5 size-4 shrink-0 text-muted-foreground"
                aria-hidden="true"
              />
              <dd class="text-sm">
                <EventsWhen v-if="occurrence" :occurrence="occurrence" :event="event" />
                <span
                  v-if="repeatText"
                  class="mt-1 flex items-center gap-1 text-xs text-muted-foreground"
                >
                  <Icon name="lucide:repeat" class="size-3" aria-hidden="true" />{{ repeatText }}
                </span>
                <span v-if="ended" class="mt-1 block text-xs text-muted-foreground">{{
                  $t("events.over")
                }}</span>
              </dd>
            </div>
            <div v-if="event.location || event.onlineUrl || event.online" class="flex gap-3">
              <dt class="sr-only">{{ $t("events.form.where") }}</dt>
              <Icon
                name="lucide:map-pin"
                class="mt-0.5 size-4 shrink-0 text-muted-foreground"
                aria-hidden="true"
              />
              <dd class="min-w-0 space-y-0.5 text-sm">
                <span v-if="event.location" class="block">{{ event.location }}</span>
                <a
                  v-if="event.onlineUrl"
                  :href="event.onlineUrl"
                  target="_blank"
                  rel="noopener noreferrer"
                  class="block truncate text-primary hover:underline"
                >
                  {{ event.onlineUrl }}
                </a>
                <span v-if="event.online" class="block text-muted-foreground">{{
                  $t("events.onlineHere")
                }}</span>
              </dd>
            </div>
          </dl>

          <!-- The event's call, which is its conversation's call. -->
          <section
            v-if="event.online && callsOffered && !cancelled"
            class="rounded-xl border bg-card p-4"
            :aria-label="$ts('events.call.title')"
          >
            <div class="flex flex-wrap items-center gap-3">
              <span class="grid size-9 place-items-center rounded-full bg-primary/10 text-primary">
                <Icon name="lucide:video" class="size-4" aria-hidden="true" />
              </span>
              <div class="min-w-48 flex-1">
                <p class="text-sm font-medium">
                  {{
                    roomCall?.participants.length
                      ? $t("events.call.live", { count: roomCall.participants.length })
                      : $t("events.call.title")
                  }}
                </p>
                <p class="text-xs text-muted-foreground">
                  <template v-if="!event.member">{{ $t("events.call.answerFirst") }}</template>
                  <template v-else-if="roomCall?.participants.length">{{
                    roomCall.participants.map((each) => nameOf(each)).join(", ")
                  }}</template>
                  <template v-else-if="event.may.startCall">{{
                    $t("events.call.youStart")
                  }}</template>
                  <template v-else>{{ $t("events.call.waitingForHost") }}</template>
                </p>
              </div>
              <template v-if="event.member && !inCall">
                <Button
                  v-if="roomCall?.participants.length || event.may.startCall"
                  variant="outline"
                  :disabled="calling.joining.value"
                  @click="startCall(false)"
                >
                  <Icon name="lucide:phone" class="size-3.5" aria-hidden="true" />
                  {{
                    roomCall?.participants.length ? $t("events.call.join") : $t("events.call.start")
                  }}
                </Button>
                <Button
                  v-if="roomCall?.participants.length || event.may.startCall"
                  :disabled="calling.joining.value"
                  @click="startCall(true)"
                >
                  <Icon name="lucide:video" class="size-3.5" aria-hidden="true" />
                  {{ $t("events.call.withVideo") }}
                </Button>
              </template>
              <span v-else-if="inCall" class="text-xs font-medium text-success">{{
                $t("events.call.youreIn")
              }}</span>
            </div>
            <p v-if="event.may.startCall" class="mt-3 text-xs text-muted-foreground">
              {{ $t("events.call.guestsHint") }}
            </p>
          </section>

          <section
            v-if="event.description"
            :aria-label="$ts('events.form.description')"
            class="text-sm leading-relaxed"
          >
            <MessageBody :body="event.description" />
          </section>

          <section v-if="event.sessions.length" aria-labelledby="agenda-heading">
            <h2 id="agenda-heading" class="text-sm font-semibold">{{ $t("events.agenda") }}</h2>
            <ol class="mt-2 divide-y rounded-xl border">
              <li v-for="session in event.sessions" :key="session.id" class="flex gap-4 px-4 py-3">
                <EventsWhen
                  class="w-28 shrink-0 text-xs"
                  :occurrence="session"
                  :event="event"
                  compact
                />
                <div class="min-w-0">
                  <p class="text-sm font-medium">{{ session.title }}</p>
                  <p
                    v-if="session.speakers.length || session.guestSpeakers"
                    class="mt-0.5 text-xs text-muted-foreground"
                  >
                    {{
                      [...session.speakers.map((each) => nameOf(each)), session.guestSpeakers]
                        .filter(Boolean)
                        .join(", ")
                    }}
                  </p>
                </div>
              </li>
            </ol>
          </section>

          <EventsAttendees ref="attendees" :event="event" />
          <EventsInvite v-if="event.may.edit && !cancelled" :event="event" />
        </div>

        <aside class="space-y-5">
          <section class="rounded-xl border bg-card p-4" :aria-label="$ts('events.yourAnswer')">
            <p class="text-xs font-semibold">{{ $t("events.yourAnswer") }}</p>
            <div
              class="mt-2 grid grid-cols-3 gap-1.5"
              role="group"
              :aria-label="$ts('events.yourAnswer')"
            >
              <button
                v-for="option in answers"
                :key="option.value"
                type="button"
                class="flex h-9 cursor-pointer items-center justify-center rounded-lg border text-xs font-medium transition-colors disabled:cursor-not-allowed disabled:opacity-50"
                :class="
                  event.myAnswer === option.value ||
                  (option.value === 'going' && event.myAnswer === 'waitlisted')
                    ? 'border-primary bg-primary text-primary-foreground'
                    : 'hover:bg-accent'
                "
                :aria-pressed="event.myAnswer === option.value"
                :disabled="busy || cancelled || ended"
                @click="answer(option.value)"
              >
                {{ option.label }}
              </button>
            </div>
            <p
              v-if="event.myAnswer === 'waitlisted'"
              class="mt-2 text-xs text-warning"
              role="status"
            >
              {{ $t("events.waitlisted", { position: event.waitlistPosition ?? 1 }) }}
            </p>
            <p class="mt-3 text-xs text-muted-foreground">
              {{ $t("events.goingCount", { count: event.counts.going }) }}
              <template v-if="event.counts.maybe">
                · {{ $t("events.maybeCount", { count: event.counts.maybe }) }}</template
              >
            </p>
            <p
              v-if="placesLeft !== null"
              class="mt-1 text-xs"
              :class="placesLeft === 0 ? 'text-warning' : 'text-muted-foreground'"
            >
              {{
                placesLeft === 0
                  ? $t("events.full", { count: event.counts.waitlisted })
                  : $t("events.placesLeft", { count: placesLeft, capacity: event.capacity ?? 0 })
              }}
            </p>
            <div
              v-if="event.member && !cancelled"
              class="mt-3 flex items-center gap-2 border-t pt-3 text-xs"
            >
              <Switch
                :model-value="!event.muted"
                :label="$ts('events.remindMe')"
                @update:model-value="setMuted(!$event)"
              />
              <span>{{ $t("events.remindMe") }}</span>
            </div>
            <p v-if="refusal" class="mt-2 text-xs text-destructive" role="alert">{{ refusal }}</p>
          </section>

          <section v-if="event.hosts.length || event.speakers.length" class="space-y-3">
            <div v-if="event.hosts.length">
              <h2 class="text-xs font-semibold text-muted-foreground">{{ $t("events.hosts") }}</h2>
              <ul class="mt-1.5 space-y-1.5">
                <li
                  v-for="person in event.hosts"
                  :key="person.userId"
                  class="flex items-center gap-2 text-sm"
                >
                  <Avatar :address="person.address" :name="nameOf(person)" size="sm" />{{
                    nameOf(person)
                  }}
                </li>
              </ul>
            </div>
            <div v-if="event.speakers.length">
              <h2 class="text-xs font-semibold text-muted-foreground">
                {{ $t("events.speakers") }}
              </h2>
              <ul class="mt-1.5 space-y-1.5">
                <li
                  v-for="person in event.speakers"
                  :key="person.userId"
                  class="flex items-center gap-2 text-sm"
                >
                  <Avatar :address="person.address" :name="nameOf(person)" size="sm" />{{
                    nameOf(person)
                  }}
                </li>
              </ul>
            </div>
          </section>

          <section class="space-y-1" :aria-label="$ts('events.addToCalendar')">
            <h2 class="text-xs font-semibold text-muted-foreground">
              {{ $t("events.addToCalendar") }}
            </h2>
            <button
              type="button"
              class="flex w-full cursor-pointer items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm hover:bg-accent"
              @click="downloadICS"
            >
              <Icon
                name="lucide:download"
                class="size-3.5 text-muted-foreground"
                aria-hidden="true"
              />
              {{ $t("events.downloadIcs") }}
            </button>
            <a
              :href="googleHref"
              target="_blank"
              rel="noopener noreferrer"
              class="flex items-center gap-2 rounded-md px-2 py-1.5 text-sm hover:bg-accent"
            >
              <Icon
                name="lucide:external-link"
                class="size-3.5 text-muted-foreground"
                aria-hidden="true"
              />
              {{ $t("events.addToGoogle") }}
            </a>
            <a
              :href="outlookHref"
              target="_blank"
              rel="noopener noreferrer"
              class="flex items-center gap-2 rounded-md px-2 py-1.5 text-sm hover:bg-accent"
            >
              <Icon
                name="lucide:external-link"
                class="size-3.5 text-muted-foreground"
                aria-hidden="true"
              />
              {{ $t("events.addToOutlook") }}
            </a>
            <NuxtLink
              :to="localeRoute({ name: 'settings', hash: '#calendar-feed' })"
              class="block px-2 pt-1 text-xs text-muted-foreground hover:text-foreground"
            >
              {{ $t("events.feed.everythingHint") }}
            </NuxtLink>
          </section>

          <section v-if="event.may.edit && !cancelled" class="space-y-1">
            <h2 class="text-xs font-semibold text-muted-foreground">{{ $t("events.cover") }}</h2>
            <input
              ref="coverInput"
              type="file"
              accept="image/*"
              class="sr-only"
              :aria-label="$ts('events.chooseCover')"
              @change="chooseCover"
            />
            <Button
              variant="outline"
              size="small"
              :disabled="uploading"
              @click="coverInput?.click()"
            >
              <Icon name="lucide:image" class="size-3.5" aria-hidden="true" />
              {{ event.cover ? $t("events.changeCover") : $t("events.chooseCover") }}
            </Button>
          </section>
        </aside>
      </div>
    </article>

    <LazyEventsForm
      v-if="editing && event"
      :event="event"
      @saved="saved"
      @close="editing = false"
    />

    <Teleport v-if="cancelling" to="body">
      <!-- eslint-disable-next-line vuejs-accessibility/click-events-have-key-events, vuejs-accessibility/no-static-element-interactions -- a backdrop click closes the dialog; Escape closes it from the keyboard through onKeyStroke -->
      <div
        class="fixed inset-0 z-50 grid place-items-center bg-black/50 p-4"
        role="dialog"
        aria-modal="true"
        :aria-label="$ts('events.cancelEvent')"
        @click.self="cancelling = false"
      >
        <div class="motion-pop-in w-full max-w-sm rounded-2xl border bg-card p-5 shadow-2xl">
          <h2 class="text-sm font-semibold">{{ $t("events.cancelTitle") }}</h2>
          <p class="mt-1 text-xs text-muted-foreground">{{ $t("events.cancelHint") }}</p>
          <label class="mt-3 block text-xs font-medium" for="cancel-reason">{{
            $t("events.cancelReason")
          }}</label>
          <Input id="cancel-reason" v-model="cancelReason" autofocus class="mt-1" />
          <div class="mt-4 flex justify-end gap-2">
            <Button variant="ghost" @click="cancelling = false">{{ $t("action.close") }}</Button>
            <Button variant="danger" @click="confirmCancel">{{ $t("events.cancelEvent") }}</Button>
          </div>
        </div>
      </div>
    </Teleport>

    <ConfirmDialog
      v-if="deleting && event"
      :title="$ts('events.deleteTitle')"
      :detail="event.title"
      :confirm-label="$ts('action.delete')"
      :cancel-label="$ts('action.cancel')"
      @confirm="confirmDelete"
      @cancel="deleting = false"
    />
  </div>
</template>
