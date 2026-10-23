<script setup lang="ts">
import { isApiError, type CalendarFeed } from "@tdc/types";
import { Button } from "#layers/ui/app/components/ui/button";
import { Input } from "#layers/ui/app/components/ui/input";
import { fully } from "#layers/ui/app/lib/dates";
import { webcalAddress } from "#layers/ui/app/lib/events";

/**
 * A personal calendar address: the events somebody is going to and their
 * channels' events, for Google Calendar, Outlook, Apple Calendar or anything
 * else that subscribes to a calendar by its address.
 *
 * The address carries a secret, shown once when it is made. Making another
 * replaces it, and revoking it stops every application that holds it.
 */
const { $t, $ts } = useI18n();
const eventsApi = useEvents();
const base = useMediaBase();
const { copy, copied } = useClipboard({ legacy: true });

const feed = ref<CalendarFeed | null>(null);
const address = ref("");
const failed = ref<string | null>(null);
const confirmingRevoke = ref(false);

async function load() {
  try {
    feed.value = await eventsApi.feed();
  } catch {
    feed.value = null;
  }
}
onMounted(load);

function absolute(path: string): string {
  const origin = base || window.location.origin;
  return `${origin}${path}`;
}

async function make() {
  failed.value = null;
  try {
    const made = await eventsApi.makeFeed();
    feed.value = made;
    address.value = made.path ? absolute(made.path) : "";
  } catch (cause) {
    failed.value = isApiError(cause) ? cause.message : $ts("events.feed.couldNotMake");
  }
}

async function revoke() {
  confirmingRevoke.value = false;
  await eventsApi.revokeFeed();
  address.value = "";
  await load();
}
</script>

<template>
  <section id="calendar-feed" aria-labelledby="calendar-feed-heading">
    <h2 id="calendar-feed-heading" class="flex items-center gap-1.5 text-sm font-semibold">
      <Icon name="lucide:calendar-plus" class="size-4" aria-hidden="true" />
      {{ $t("events.feed.title") }}
    </h2>
    <p class="mt-1 text-xs text-muted-foreground">{{ $t("events.feed.hint") }}</p>

    <div v-if="address" class="mt-3 space-y-2 rounded-xl border bg-card p-3">
      <p class="text-xs font-medium text-warning">{{ $t("events.feed.shownOnce") }}</p>
      <div class="flex gap-2">
        <Input
          :model-value="address"
          readonly
          :label="$ts('events.feed.address')"
          class="font-mono text-xs"
        />
        <Button variant="outline" @click="copy(address)">{{
          copied ? $t("events.feed.copied") : $t("events.feed.copy")
        }}</Button>
      </div>
      <a
        :href="webcalAddress(address)"
        class="inline-flex items-center gap-1 text-xs text-primary hover:underline"
      >
        <Icon name="lucide:calendar-check" class="size-3.5" aria-hidden="true" />{{
          $t("events.feed.openInApp")
        }}
      </a>
      <details class="text-xs text-muted-foreground">
        <summary class="cursor-pointer text-foreground">{{ $t("events.feed.howTo") }}</summary>
        <ul class="mt-2 list-disc space-y-1 pl-4">
          <li>{{ $t("events.feed.google") }}</li>
          <li>{{ $t("events.feed.outlook") }}</li>
          <li>{{ $t("events.feed.apple") }}</li>
        </ul>
      </details>
    </div>

    <p v-else-if="feed?.exists" class="mt-3 text-xs text-muted-foreground">
      {{ $t("events.feed.exists", { when: feed.createdAt ? fully(feed.createdAt) : "" }) }}
      <template v-if="feed.lastUsedAt">{{
        $t("events.feed.lastUsed", { when: fully(feed.lastUsedAt) })
      }}</template>
    </p>

    <div class="mt-3 flex flex-wrap gap-2">
      <Button variant="outline" @click="make">
        {{ feed?.exists ? $t("events.feed.replace") : $t("events.feed.make") }}
      </Button>
      <Button v-if="feed?.exists" variant="danger" @click="confirmingRevoke = true">{{
        $t("events.feed.revoke")
      }}</Button>
    </div>
    <p v-if="failed" class="mt-2 text-xs text-destructive" role="alert">{{ failed }}</p>

    <ConfirmDialog
      v-if="confirmingRevoke"
      :title="$ts('events.feed.revokeTitle')"
      :detail="$ts('events.feed.revokeDetail')"
      :confirm-label="$ts('events.feed.revoke')"
      :cancel-label="$ts('action.cancel')"
      @confirm="revoke"
      @cancel="confirmingRevoke = false"
    />
  </section>
</template>
