<script setup lang="ts">
import { isApiError, type CalendarEvent, type EventDetail, type Room } from "@tdc/types";
import { addDays, format } from "date-fns";
import { Button } from "#layers/ui/app/components/ui/button";
import { Select } from "#layers/ui/app/components/ui/select";
import {
  byDay,
  dayKey,
  monthGrid,
  shiftDay,
  shiftMonth,
  startOfDayIn,
  weekOf,
  ALL_CHANNELS,
} from "#layers/ui/app/lib/events";

/**
 * Every event this person can see: as an agenda of what is coming (or what
 * has been), as a month, or as a week. Each occurrence of a repeating event is
 * its own entry, at its own time in the reader's zone.
 */
definePageMeta({ middleware: "auth", layout: "app" });

const { $t, $ts, $localeRoute: localeRoute } = useI18n();
useHead({ title: $ts("events.title") });

const eventsApi = useEvents();
const roomsApi = useRooms();
const { can } = useAuth();
const route = useRoute();
const router = useRouter();

type View = "agenda" | "month" | "week";
const view = ref<View>((route.query.view as View) || "agenda");
const past = ref(route.query.when === "past");
const channel = ref(String(route.query.channel ?? ALL_CHANNELS));
const today = dayKey(new Date());
const month = ref(today.slice(0, 7));
const day = ref(today);

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

watch([view, past, channel], () => {
  void router.replace({
    query: {
      ...(view.value !== "agenda" ? { view: view.value } : {}),
      ...(past.value ? { when: "past" } : {}),
      ...(channel.value !== ALL_CHANNELS ? { channel: channel.value } : {}),
    },
  });
});

/** The span the current view covers, as instants for the server. */
const range = computed(() => {
  if (view.value === "month") {
    const days = monthGrid(month.value);
    return { from: startOfDayIn(days[0]!), to: startOfDayIn(shiftDay(days.at(-1)!, 1)) };
  }
  if (view.value === "week") {
    const days = weekOf(day.value);
    return { from: startOfDayIn(days[0]!), to: startOfDayIn(shiftDay(days[6]!, 1)) };
  }
  const now = new Date();
  return past.value
    ? { from: addDays(now, -365).toISOString(), to: now.toISOString() }
    : { from: now.toISOString(), to: addDays(now, 180).toISOString() };
});

const events = ref<CalendarEvent[]>([]);
const loading = ref(true);
const failed = ref<string | null>(null);

async function load() {
  loading.value = true;
  failed.value = null;
  try {
    const listed = await eventsApi.list({
      ...range.value,
      channel: channel.value === ALL_CHANNELS ? undefined : channel.value,
    });
    events.value = past.value && view.value === "agenda" ? listed.reverse() : listed;
  } catch (cause) {
    failed.value = isApiError(cause) ? cause.message : $ts("events.couldNotLoad");
  } finally {
    loading.value = false;
  }
}
watch(range, () => void load(), { immediate: true });
watch(channel, () => void load());

const agenda = computed(() => [...byDay(events.value).entries()]);

function dayHeading(key: string): string {
  if (key === today) return $ts("events.today");
  if (key === dayKey(addDays(new Date(), 1))) return $ts("events.tomorrow");
  return format(new Date(`${key}T12:00:00`), "EEEE d MMMM");
}

const heading = computed(() => {
  if (view.value === "month") return format(new Date(`${month.value}-15T12:00:00`), "MMMM yyyy");
  if (view.value === "week") {
    const days = weekOf(day.value);
    return `${format(new Date(`${days[0]}T12:00:00`), "d MMM")} – ${format(new Date(`${days[6]}T12:00:00`), "d MMM yyyy")}`;
  }
  return "";
});

function step(by: number) {
  if (view.value === "month") month.value = shiftMonth(month.value, by);
  else day.value = shiftDay(day.value, by * 7);
}
function backToToday() {
  month.value = today.slice(0, 7);
  day.value = today;
}

function openDay(key: string) {
  day.value = key;
  view.value = "week";
}

const views = computed(() => [
  { value: "agenda" as const, label: $ts("events.views.agenda"), icon: "list" },
  { value: "month" as const, label: $ts("events.views.month"), icon: "grid" },
  { value: "week" as const, label: $ts("events.views.week"), icon: "columns" },
]);
const channelOptions = computed(() => [
  { value: ALL_CHANNELS, label: $ts("events.allChannels") },
  ...channels.value.map((room) => ({ value: room.id, label: `#${room.name}` })),
]);

const scheduling = ref(false);
const importing = ref(false);
const following = ref(false);

async function scheduled(event: EventDetail) {
  scheduling.value = false;
  await router.push(localeRoute({ name: "events-id", params: { id: event.id } }));
}
</script>

<template>
  <div class="flex-1 overflow-y-auto">
    <header
      class="sticky top-0 z-10 flex h-12 shrink-0 items-center gap-2 border-b bg-background/95 px-4 backdrop-blur"
    >
      <h1 class="text-sm font-semibold">{{ $t("events.title") }}</h1>

      <div
        class="ml-3 hidden items-center rounded-lg border p-0.5 sm:flex"
        role="group"
        :aria-label="$ts('events.views.label')"
      >
        <button
          v-for="option in views"
          :key="option.value"
          type="button"
          class="h-6 cursor-pointer rounded-md px-2 text-xs transition-colors"
          :class="
            view === option.value
              ? 'bg-accent font-medium text-foreground'
              : 'text-muted-foreground hover:text-foreground'
          "
          :aria-pressed="view === option.value"
          @click="view = option.value"
        >
          {{ option.label }}
        </button>
      </div>

      <div class="ml-auto flex items-center gap-1.5">
        <Button v-if="can('event.create')" variant="ghost" size="small" @click="importing = true">
          <Icon name="lucide:upload" class="size-3.5" aria-hidden="true" />
          <span class="max-sm:sr-only">{{ $t("events.import.title") }}</span>
        </Button>
        <Button
          v-if="can('event.calendars.manage')"
          variant="ghost"
          size="small"
          @click="following = true"
        >
          <Icon name="lucide:rss" class="size-3.5" aria-hidden="true" />
          <span class="max-sm:sr-only">{{ $t("events.subscriptions.title") }}</span>
        </Button>
        <NuxtLink
          :to="localeRoute({ name: 'settings', hash: '#calendar-feed' })"
          class="btn btn-ghost h-6 px-1.5 text-xs"
        >
          <Icon name="lucide:calendar-plus" class="size-3.5" aria-hidden="true" />
          <span class="max-sm:sr-only">{{ $t("events.feed.short") }}</span>
        </NuxtLink>
        <Button v-if="can('event.create')" @click="scheduling = true">
          <Icon name="lucide:plus" class="size-3.5" aria-hidden="true" />
          {{ $t("events.new") }}
        </Button>
      </div>
    </header>

    <div class="mx-auto max-w-5xl space-y-4 px-4 py-5">
      <div class="flex flex-wrap items-center gap-2">
        <div
          class="flex items-center rounded-lg border p-0.5 sm:hidden"
          role="group"
          :aria-label="$ts('events.views.label')"
        >
          <button
            v-for="option in views"
            :key="option.value"
            type="button"
            class="h-6 cursor-pointer rounded-md px-2 text-xs"
            :class="view === option.value ? 'bg-accent font-medium' : 'text-muted-foreground'"
            :aria-pressed="view === option.value"
            @click="view = option.value"
          >
            {{ option.label }}
          </button>
        </div>

        <template v-if="view === 'agenda'">
          <div
            class="flex items-center rounded-lg border p-0.5"
            role="group"
            :aria-label="$ts('events.when')"
          >
            <button
              type="button"
              class="h-6 cursor-pointer rounded-md px-2 text-xs"
              :class="
                !past ? 'bg-accent font-medium' : 'text-muted-foreground hover:text-foreground'
              "
              :aria-pressed="!past"
              @click="past = false"
            >
              {{ $t("events.upcoming") }}
            </button>
            <button
              type="button"
              class="h-6 cursor-pointer rounded-md px-2 text-xs"
              :class="
                past ? 'bg-accent font-medium' : 'text-muted-foreground hover:text-foreground'
              "
              :aria-pressed="past"
              @click="past = true"
            >
              {{ $t("events.past") }}
            </button>
          </div>
        </template>
        <template v-else>
          <Button variant="outline" size="icon" :label="$ts('events.previous')" @click="step(-1)">
            <Icon name="lucide:chevron-left" class="size-4" aria-hidden="true" />
          </Button>
          <Button variant="outline" size="icon" :label="$ts('events.next')" @click="step(1)">
            <Icon name="lucide:chevron-right" class="size-4" aria-hidden="true" />
          </Button>
          <Button variant="ghost" size="small" @click="backToToday">{{
            $t("events.today")
          }}</Button>
          <h2 class="text-sm font-semibold" aria-live="polite">{{ heading }}</h2>
        </template>

        <!-- eslint-disable-next-line vuejs-accessibility/form-control-has-label -- the Select component carries its label as aria-label, which the rule cannot see inside -->
        <Select
          v-model="channel"
          class="ml-auto w-48"
          :label="$ts('events.filterChannel')"
          :options="channelOptions"
        />
      </div>

      <p v-if="failed" class="text-sm text-destructive" role="alert">{{ failed }}</p>

      <div v-else-if="loading && !events.length" class="space-y-2" aria-busy="true">
        <UiSkeleton v-for="n in 4" :key="n" class="h-14 w-full rounded-lg" />
      </div>

      <EventsMonth v-else-if="view === 'month'" :month="month" :events="events" @day="openDay" />
      <EventsWeek v-else-if="view === 'week'" :day="day" :events="events" />

      <div
        v-else-if="!events.length"
        class="rounded-xl border border-dashed px-6 py-12 text-center"
      >
        <Icon
          name="lucide:calendar-days"
          class="mx-auto size-6 text-muted-foreground"
          aria-hidden="true"
        />
        <p class="mt-2 text-sm font-medium">
          {{ past ? $t("events.noneBefore") : $t("events.noneYet") }}
        </p>
        <p class="mt-1 text-xs text-muted-foreground">{{ $t("events.noneYetHint") }}</p>
        <Button v-if="can('event.create') && !past" class="mt-4" @click="scheduling = true">
          {{ $t("events.new") }}
        </Button>
      </div>

      <div v-else class="space-y-5">
        <section v-for="[key, items] in agenda" :key="key" :aria-labelledby="`day-${key}`">
          <h2 :id="`day-${key}`" class="px-3 text-xs font-semibold text-muted-foreground">
            {{ dayHeading(key) }}
          </h2>
          <ul class="mt-1">
            <li v-for="event in items" :key="`${event.id}-${event.occurrenceStartsAt}`">
              <EventsCard :event="event" />
            </li>
          </ul>
        </section>
      </div>
    </div>

    <LazyEventsForm
      v-if="scheduling"
      :channel-id="channel === ALL_CHANNELS ? undefined : channel"
      @saved="scheduled"
      @close="scheduling = false"
    />
    <LazyEventsImport v-if="importing" @close="importing = false" @imported="load()" />
    <LazyEventsSubscriptions v-if="following" @close="((following = false), load())" />
  </div>
</template>
