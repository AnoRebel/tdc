<script setup lang="ts">
import { Check, FilePen, GitCompareArrows, History, Undo2, X } from "lucide-vue-next";

import type {
  Agreement,
  AgreementRevision,
  Engagement,
  Negotiation,
  ProposalVersion,
} from "@tdc/types";
import { isApiError } from "@tdc/types";
import { Button } from "#layers/ui/app/components/ui/button";
import { fully } from "#layers/ui/app/lib/dates";
import ChangeRequestForm from "#layers/ui/app/components/directory/ChangeRequestForm.vue";
import ProposalForm from "#layers/ui/app/components/directory/ProposalForm.vue";
import ProposalTerms from "#layers/ui/app/components/directory/ProposalTerms.vue";
import TermsDiff from "#layers/ui/app/components/directory/TermsDiff.vue";

const { $t, $ts } = useI18n();

/**
 * Revising the agreement in force, and the history of how it came to be.
 *
 * The same rules as the first agreement: a revision is versions nobody can
 * rewrite, answered by accepting, declining or asking for a change, and it
 * is in force only once both people accept the same version. Until then the
 * agreement above stays the one in force, and this says so.
 *
 * What changes is shown against the agreement in force, part by part, so the
 * person deciding sees the difference rather than two sets of terms to
 * compare. The server decides everything; this only offers the buttons that
 * make sense to the person looking.
 */

const props = defineProps<{ engagement: Engagement; negotiation: Negotiation }>();
const emit = defineEmits<{ changed: [Engagement]; reload: [] }>();

const directory = useDirectory();
const error = ref<string | null>(null);
const busy = ref(false);
/** "open" while proposing a revision, "counter" while sending a new version of one. */
const composing = ref<"open" | "counter" | null>(null);
const askingChange = ref(false);

const id = computed(() => props.engagement.id);
const me = computed(() =>
  props.engagement.you === "client"
    ? props.engagement.client.username
    : props.engagement.developer.username,
);
const them = computed(() =>
  props.engagement.you === "client"
    ? props.engagement.developer.username
    : props.engagement.client.username,
);
/** What to call the other side, where they are named rather than compared. */
const themName = computed(() =>
  props.engagement.you === "client"
    ? props.engagement.developer.displayName
    : props.engagement.client.displayName,
);

const revisions = computed(() => props.negotiation.revisions ?? []);
const agreements = computed(() => props.negotiation.agreements ?? []);
const inForce = computed(() => props.negotiation.agreement);
const open = computed(() => revisions.value.find((revision) => revision.outcome === "open"));
const versionsIn = (number: number) =>
  props.negotiation.versions.filter((version) => version.revision === number);
const openLatest = computed(() => (open.value ? versionsIn(open.value.number).at(-1) : undefined));
const responsesTo = (version: number) =>
  props.negotiation.responses.filter((response) => response.version === version);

const accepted = computed(() =>
  openLatest.value
    ? responsesTo(openLatest.value.version)
        .filter((response) => response.kind === "accept")
        .map((response) => response.responder)
    : [],
);
const changeAsked = computed(() =>
  openLatest.value
    ? responsesTo(openLatest.value.version).some((response) => response.kind === "request_changes")
    : false,
);
const youAccepted = computed(() => accepted.value.includes(me.value));
const theyAccepted = computed(() => accepted.value.includes(them.value));
const youOpened = computed(() => open.value?.openedBy === me.value);
const revisable = computed(() =>
  ["agreement", "active", "completion_pending"].includes(props.engagement.state),
);

function partLabel(part?: string) {
  if (!part) return "";
  if (part.startsWith("item:"))
    return $ts("directory.agreement.parts.item", { number: part.slice(5) });
  return $ts(`directory.agreement.parts.${part}`);
}

async function run(action: () => Promise<Engagement | unknown>) {
  if (busy.value) return;
  busy.value = true;
  error.value = null;
  try {
    const result = await action();
    if (result && typeof result === "object" && "state" in result)
      emit("changed", result as Engagement);
    emit("reload");
  } catch (cause) {
    error.value = isApiError(cause) ? cause.message : $ts("directory.couldNotReach");
  } finally {
    busy.value = false;
  }
}

function answer(kind: "accept" | "decline") {
  const version = openLatest.value;
  if (version) void run(() => directory.respond(id.value, version.id, kind));
}
function requestChange(part: string, comment: string) {
  const version = openLatest.value;
  askingChange.value = false;
  if (version)
    void run(() => directory.respond(id.value, version.id, "request_changes", part, comment));
}
function withdraw() {
  const revision = open.value;
  if (revision) void run(() => directory.withdrawRevision(id.value, revision.number));
}
async function sent() {
  composing.value = null;
  emit("changed", await directory.engagement(id.value));
  emit("reload");
}

/** The agreement that was in force just before a given version was sent. */
function inForceBefore(version: number): Agreement | undefined {
  return agreements.value.filter((agreement) => agreement.version < version).at(-1);
}

/** The terms a version is compared with: the previous version of its revision, or the agreement it revises. */
function baselineOf(version: ProposalVersion): ProposalVersion | undefined {
  const siblings = versionsIn(version.revision ?? 0);
  const index = siblings.findIndex((candidate) => candidate.id === version.id);
  return index > 0 ? siblings[index - 1] : inForceBefore(version.version)?.terms;
}

interface Moment {
  key: string;
  at: string;
  text: string;
  note?: string;
  version?: ProposalVersion;
  tone: "agreed" | "neutral" | "ended";
}

/** Everything that happened to the agreement, oldest first. */
const timeline = computed<Moment[]>(() => {
  const moments: Moment[] = [];
  for (const agreement of agreements.value) {
    moments.push({
      key: `agreement-${agreement.version}`,
      at: agreement.recordedAt,
      tone: "agreed",
      text:
        agreement.revision > 0
          ? $ts("directory.revision.history.inForce", {
              number: agreement.revision,
              version: agreement.version,
            })
          : $ts("directory.revision.history.agreed", { version: agreement.version }),
    });
  }
  for (const revision of revisions.value) {
    const versions = versionsIn(revision.number);
    versions.forEach((version, index) => {
      moments.push({
        key: `version-${version.version}`,
        at: version.sentAt,
        tone: "neutral",
        version,
        note: index === 0 ? revision.reason : undefined,
        text:
          index === 0
            ? $ts("directory.revision.history.opened", {
                who: revision.openedBy,
                number: revision.number,
                version: version.version,
              })
            : $ts("directory.revision.history.counter", {
                who: version.sentBy,
                number: revision.number,
                version: version.version,
              }),
      });
      for (const [position, response] of responsesTo(version.version).entries()) {
        moments.push({
          key: `response-${version.version}-${position}`,
          at: response.at,
          tone: response.kind === "decline" ? "ended" : "neutral",
          note: response.comment,
          text: $ts(`directory.revision.history.${response.kind}`, {
            who: response.responder,
            version: version.version,
            part: partLabel(response.part),
          }),
        });
      }
    });
    if (revision.outcome === "withdrawn" && revision.outcomeAt) {
      moments.push({
        key: `withdrawn-${revision.number}`,
        at: revision.outcomeAt,
        tone: "ended",
        note: revision.outcomeNote,
        text: $ts("directory.revision.history.withdrawn", {
          who: revision.outcomeBy ?? "",
          number: revision.number,
        }),
      });
    }
  }
  // Times are to the second, so an agreement and the acceptance that made it
  // share one; the agreement reads after it.
  const rank = (moment: Moment) => (moment.tone === "agreed" ? 1 : 0);
  return moments.sort((a, b) => a.at.localeCompare(b.at) || rank(a) - rank(b));
});

const outcomeLabel = (revision: AgreementRevision) =>
  $ts(`directory.revision.outcome.${revision.outcome}`);
</script>

<template>
  <section class="space-y-3" :aria-label="$ts('directory.revision.title')" data-revisions>
    <p
      v-if="error"
      class="rounded-md bg-destructive/10 px-3 py-2 text-sm text-destructive"
      role="alert"
    >
      {{ error }}
    </p>

    <!-- The revision under negotiation: why, what it changes, and who has accepted. -->
    <div
      v-if="open && openLatest && composing !== 'counter'"
      class="space-y-3 rounded-lg border border-amber-500/50 p-3"
      data-open-revision
    >
      <div class="space-y-1">
        <h2 class="flex flex-wrap items-center gap-2 text-sm font-semibold">
          <GitCompareArrows class="size-4" />
          {{ $t("directory.revision.openTitle", { number: open.number }) }}
          <span
            class="rounded-full bg-amber-500/15 px-2 py-0.5 text-xs font-medium text-amber-700 dark:text-amber-300"
          >
            {{ $t("directory.revision.outcome.open") }}
          </span>
        </h2>
        <p class="text-xs text-muted-foreground">
          {{ $t("directory.revision.proposedBy", { who: open.openedBy }) }} ·
          <time :datetime="open.openedAt">{{ fully(open.openedAt) }}</time>
        </p>
        <blockquote class="border-l-2 pl-2 text-sm whitespace-pre-line" data-revision-reason>
          {{ open.reason }}
        </blockquote>
      </div>

      <p class="rounded-md bg-muted px-3 py-2 text-xs" role="status">
        {{ $t("directory.revision.stillInForce", { version: inForce?.version ?? 0 }) }}
      </p>

      <div class="space-y-1">
        <h3 class="text-xs font-semibold">
          {{ $t("directory.revision.whatChanges", { version: openLatest.version }) }}
          <span class="font-normal text-muted-foreground">
            · {{ $t("directory.agreement.sentBy", { who: openLatest.sentBy }) }}
          </span>
        </h3>
        <TermsDiff v-if="inForce" :before="inForce.terms" :after="openLatest" />
      </div>

      <details class="text-sm">
        <summary class="cursor-pointer text-xs text-muted-foreground hover:text-foreground">
          {{ $t("directory.revision.fullTerms", { version: openLatest.version }) }}
        </summary>
        <div class="mt-2"><ProposalTerms :version="openLatest" /></div>
      </details>

      <ul v-if="responsesTo(openLatest.version).length" class="space-y-1 text-xs">
        <li v-for="(response, index) in responsesTo(openLatest.version)" :key="index">
          {{
            $t(`directory.agreement.responses.${response.kind}`, {
              who: response.responder,
              part: partLabel(response.part),
            })
          }}
          <span v-if="response.comment" class="text-muted-foreground"
            >— {{ response.comment }}</span
          >
        </li>
      </ul>

      <p v-if="changeAsked" class="text-xs text-muted-foreground" role="status">
        {{ $t("directory.revision.waitingVersion") }}
      </p>
      <p v-else-if="youAccepted" class="text-xs" role="status" data-you-accepted>
        {{ $t("directory.revision.youAccepted", { name: themName }) }}
      </p>
      <p v-else-if="theyAccepted" class="text-xs" role="status">
        {{ $t("directory.revision.theyAccepted", { name: themName }) }}
      </p>

      <div v-if="!askingChange" class="flex flex-wrap gap-2">
        <Button
          v-if="!youAccepted && !changeAsked"
          :disabled="busy"
          data-accept-revision
          @click="answer('accept')"
        >
          <Check class="size-3.5" />
          {{ $t("directory.revision.accept") }}
        </Button>
        <Button v-if="!changeAsked" variant="outline" :disabled="busy" @click="askingChange = true">
          {{ $t("directory.agreement.askChange") }}
        </Button>
        <Button
          variant="outline"
          :disabled="busy"
          data-counter-revision
          @click="composing = 'counter'"
        >
          <FilePen class="size-3.5" />
          {{ $t("directory.revision.counter") }}
        </Button>
        <Button
          v-if="youOpened"
          variant="ghost"
          :disabled="busy"
          data-withdraw-revision
          @click="withdraw"
        >
          <Undo2 class="size-3.5" />
          {{ $t("directory.revision.withdraw") }}
        </Button>
        <Button
          v-else
          variant="ghost"
          :disabled="busy"
          data-decline-revision
          @click="answer('decline')"
        >
          <X class="size-3.5" />
          {{ $t("directory.revision.decline") }}
        </Button>
      </div>
      <ChangeRequestForm
        v-else
        :version="openLatest"
        @send="requestChange"
        @cancel="askingChange = false"
      />
    </div>

    <!-- Writing: a new revision from the terms in force, or a counter-version from the latest. -->
    <div v-if="composing" class="space-y-2 rounded-lg border p-3" data-revision-form>
      <h2 class="text-sm font-semibold">
        {{
          composing === "open"
            ? $t("directory.revision.proposeTitle")
            : $t("directory.revision.counterTitle", { number: open?.number ?? 0 })
        }}
      </h2>
      <p class="text-xs text-muted-foreground">
        {{
          composing === "open"
            ? $t("directory.revision.proposeHint")
            : $t("directory.revision.counterHint")
        }}
      </p>
      <ProposalForm
        :engagement-id="id"
        :from="composing === 'open' ? inForce?.terms : openLatest"
        :revising="composing === 'open'"
        @sent="sent"
        @cancel="composing = null"
      />
    </div>
    <div v-else-if="!open && revisable && inForce" class="flex flex-wrap items-center gap-3">
      <Button variant="outline" data-propose-revision @click="composing = 'open'">
        <FilePen class="size-3.5" />
        {{ $t("directory.revision.propose") }}
      </Button>
      <p class="text-xs text-muted-foreground">{{ $t("directory.revision.proposeExplainer") }}</p>
    </div>

    <!-- How the agreement in force came to be. -->
    <details v-if="revisions.length" class="rounded-lg border p-3" open data-revision-history>
      <summary class="flex cursor-pointer items-center gap-2 text-sm font-semibold">
        <History class="size-4" />
        {{ $t("directory.revision.historyTitle") }}
      </summary>
      <ul class="mt-2 flex flex-wrap gap-2 text-xs" :aria-label="$ts('directory.revision.title')">
        <li
          v-for="revision in revisions"
          :key="revision.number"
          class="rounded-full border px-2 py-0.5"
        >
          {{
            $t("directory.revision.chip", {
              number: revision.number,
              outcome: outcomeLabel(revision),
            })
          }}
        </li>
      </ul>
      <ol class="mt-3 space-y-3 border-l pl-4">
        <li
          v-for="moment in timeline"
          :key="moment.key"
          class="relative space-y-1 text-sm"
          :data-moment="moment.key"
        >
          <span
            class="absolute top-1.5 -left-[1.3rem] size-2 rounded-full"
            :class="{
              'bg-primary': moment.tone === 'agreed',
              'bg-muted-foreground/50': moment.tone === 'neutral',
              'bg-destructive/70': moment.tone === 'ended',
            }"
            aria-hidden="true"
          />
          <p>
            {{ moment.text }}
            <time class="ml-1 text-xs text-muted-foreground" :datetime="moment.at">{{
              fully(moment.at)
            }}</time>
          </p>
          <p v-if="moment.note" class="text-xs whitespace-pre-line text-muted-foreground">
            “{{ moment.note }}”
          </p>
          <details v-if="moment.version && baselineOf(moment.version)" class="text-xs">
            <summary class="cursor-pointer text-muted-foreground hover:text-foreground">
              {{ $t("directory.revision.showChanges") }}
            </summary>
            <div class="mt-2">
              <TermsDiff :before="baselineOf(moment.version)!" :after="moment.version" />
            </div>
          </details>
        </li>
      </ol>
    </details>
  </section>
</template>
