<script setup lang="ts">
import type { Engagement } from "@tdc/types";
import { isApiError } from "@tdc/types";
import { ArrowLeft, ChevronDown, History, SearchX } from "lucide-vue-next";
import { Button } from "#layers/ui/app/components/ui/button";
import { fully } from "#layers/ui/app/lib/dates";

const { $t, $ts, $localeRoute: localeRoute } = useI18n();

/**
 * One engagement: where it stands, and the conversation it is happening in.
 *
 * Both on one screen, because the state is what the conversation is about. The
 * state comes from the same record for both people, and when either changes it
 * the other's page moves with it, over the live connection the conversation
 * already uses — the change is announced in the conversation's own log, so a
 * page that was offline when it happened catches up like any message.
 */

definePageMeta({ middleware: "auth", layout: "app" });

const route = useRoute();
const directory = useDirectory();

const id = computed(() => String(route.params.id));
const engagement = ref<Engagement | null>(null);
const missing = ref(false);
const error = ref<string | null>(null);
const showHistory = ref(false);
const withdrawing = ref(false);

useHead({ title: () => engagement.value?.title ?? $ts("directory.engagement.title") });

async function load() {
  try {
    engagement.value = await directory.engagement(id.value);
    missing.value = false;
  } catch (cause) {
    if (isApiError(cause) && cause.code === "not-found") missing.value = true;
    else error.value = isApiError(cause) ? cause.message : $ts("directory.couldNotReach");
  }
}

await load();

/*
 * Which tab is open. The proposal once there is one to talk about; before
 * that, the conversation, which is where an engagement starts.
 */
const tabs = ["conversation", "proposal"] as const;
const revision = ref(0);
const view = ref<(typeof tabs)[number]>(
  engagement.value && engagement.value.state !== "discussion" ? "proposal" : "conversation",
);

/*
 * Following the other person's changes.
 *
 * A connection of its own, subscribed to the engagement's conversation and
 * listening only for the notice that the engagement moved. The conversation's
 * messages are the conversation component's business.
 */
const live = useLiveConnection({
  onEvent(roomId, _sequence, payload) {
    if (roomId !== engagement.value?.roomId) return;
    if (payload.type === "engagement.changed") {
      revision.value++;
      void load();
    }
  },
  onResync() {
    revision.value++;
    void load();
  },
});
watch(
  () => engagement.value?.roomId,
  (roomId, previous) => {
    if (previous) live.unsubscribe(previous);
    if (roomId) live.subscribe(roomId);
  },
  { immediate: true },
);
onBeforeUnmount(() => live.close());

const otherParty = computed(() =>
  engagement.value?.you === "client"
    ? engagement.value.developer.username
    : (engagement.value?.client.username ?? ""),
);

async function confirmWithdraw(reason: string) {
  withdrawing.value = false;
  try {
    engagement.value = await directory.withdrawEngagement(id.value, reason);
  } catch (cause) {
    error.value = isApiError(cause) ? cause.message : $ts("directory.couldNotReach");
  }
}

// The steps in order, for showing how far along it is.
const steps = ["discussion", "proposal", "agreement", "active", "completed"] as const;
const reached = computed(() => {
  const state = engagement.value?.state;
  if (!state || state === "withdrawn") return -1;
  const order: Record<string, number> = {
    discussion: 0,
    proposal: 1,
    negotiation: 1,
    agreement: 2,
    active: 3,
    completion_pending: 3,
    completed: 4,
  };
  return order[state] ?? 0;
});
</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col">
    <header class="flex h-12 shrink-0 items-center gap-3 border-b px-4">
      <NuxtLink
        :to="localeRoute({ name: 'directory-engagements' })"
        class="btn btn-ghost size-7 px-0"
        :aria-label="$ts('directory.engagement.backToList')"
      >
        <ArrowLeft class="size-4" />
      </NuxtLink>
      <h1 class="truncate text-sm font-semibold">
        {{ engagement?.title ?? $t("directory.engagement.title") }}
      </h1>
    </header>

    <div v-if="missing" class="m-4 rounded-lg border border-dashed py-12 text-center">
      <SearchX class="mx-auto size-6 text-muted-foreground/60" />
      <p class="mt-2 text-sm font-medium">{{ $t("directory.engagement.notFound") }}</p>
    </div>

    <template v-else-if="engagement">
      <section
        class="shrink-0 space-y-3 border-b px-4 py-3"
        :aria-label="$ts('directory.engagement.whereItStands')"
      >
        <p
          v-if="error"
          class="rounded-md bg-destructive/10 px-3 py-2 text-sm text-destructive"
          role="alert"
        >
          {{ error }}
        </p>

        <div class="flex flex-wrap items-center gap-2">
          <span
            class="rounded-full bg-primary/10 px-2.5 py-0.5 text-xs font-medium"
            data-engagement-state
            role="status"
          >
            {{ $t(`directory.engagement.states.${engagement.state}`) }}
          </span>
          <span class="text-xs text-muted-foreground">
            {{ $t("directory.engagement.with", { name: otherParty }) }}
          </span>
          <Button
            v-if="engagement.withdrawable"
            variant="ghost"
            class="ml-auto"
            @click="withdrawing = true"
          >
            {{ $t("directory.engagement.withdraw") }}
          </Button>
        </div>

        <!-- How far along, as steps rather than a percentage: the stages are
             what people talk about. -->
        <ol v-if="engagement.state !== 'withdrawn'" class="flex flex-wrap gap-x-3 gap-y-1 text-xs">
          <li
            v-for="(step, index) in steps"
            :key="step"
            :class="index <= reached ? 'font-medium text-foreground' : 'text-muted-foreground'"
            :aria-current="index === reached ? 'step' : undefined"
          >
            {{ index + 1 }}. {{ $t(`directory.engagement.states.${step}`) }}
          </li>
        </ol>
        <p v-else class="text-xs text-muted-foreground">
          {{ $t("directory.engagement.withdrawnHint") }}
        </p>

        <button
          type="button"
          class="flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
          :aria-expanded="showHistory"
          @click="showHistory = !showHistory"
        >
          <History class="size-3.5" />
          {{ $t("directory.engagement.history") }}
          <ChevronDown class="size-3.5 transition-transform" :class="showHistory && 'rotate-180'" />
        </button>
        <ol v-if="showHistory" class="space-y-1 text-xs" data-engagement-history>
          <li v-for="(step, index) in engagement.transitions" :key="index">
            <time class="text-muted-foreground" :datetime="step.at">{{ fully(step.at) }}</time>
            ·
            {{
              $t("directory.engagement.moved", {
                who: step.actor,
                to: $ts(`directory.engagement.states.${step.to}`),
              })
            }}
            <span v-if="step.note" class="text-muted-foreground">— {{ step.note }}</span>
          </li>
        </ol>
      </section>

      <!-- The conversation and the proposal side by side as tabs: on a phone
           there is room for one at a time, and the conversation stays mounted
           so it keeps receiving while the proposal is open. -->
      <div
        class="flex shrink-0 gap-1 border-b px-4 py-1.5"
        role="tablist"
        :aria-label="$ts('directory.engagement.title')"
      >
        <button
          v-for="tab in tabs"
          :id="`engagement-tab-${tab}`"
          :key="tab"
          type="button"
          role="tab"
          class="rounded-md px-3 py-1 text-sm"
          :class="view === tab ? 'bg-muted font-medium' : 'text-muted-foreground'"
          :aria-selected="view === tab"
          :aria-controls="`engagement-panel-${tab}`"
          @click="view = tab"
        >
          {{ $t(`directory.agreement.tabs.${tab}`) }}
        </button>
      </div>

      <div
        v-if="view === 'proposal'"
        id="engagement-panel-proposal"
        class="min-h-0 flex-1 overflow-y-auto px-4 py-3"
        role="tabpanel"
        aria-labelledby="engagement-tab-proposal"
      >
        <Suspense>
          <DirectoryNegotiation
            :engagement="engagement"
            :revision="revision"
            @changed="engagement = $event"
          />
        </Suspense>
      </div>
      <div
        v-show="view === 'conversation'"
        id="engagement-panel-conversation"
        class="flex min-h-0 flex-1 flex-col"
        role="tabpanel"
        aria-labelledby="engagement-tab-conversation"
      >
        <ChatRoom v-if="engagement.roomId" :room-id="engagement.roomId" />
      </div>
    </template>

    <DirectoryWithdrawDialog
      v-if="withdrawing"
      @cancel="withdrawing = false"
      @confirm="confirmWithdraw"
    />
  </div>
</template>
