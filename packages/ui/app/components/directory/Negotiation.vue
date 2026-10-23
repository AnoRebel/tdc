<script setup lang="ts">
import { useForm } from "@tanstack/vue-form";
import * as v from "valibot";
import { Check, ChevronDown, Download, FilePen, Handshake, Info, X } from "lucide-vue-next";

import type { Engagement, Negotiation, ProposalVersion } from "@tdc/types";
import { isApiError } from "@tdc/types";
import { Button } from "#layers/ui/app/components/ui/button";
import { Field } from "#layers/ui/app/components/ui/field";
import { Textarea } from "#layers/ui/app/components/ui/textarea";
import { fully } from "#layers/ui/app/lib/dates";
import AgreementRevisions from "#layers/ui/app/components/directory/AgreementRevisions.vue";
import ChangeRequestForm from "#layers/ui/app/components/directory/ChangeRequestForm.vue";
import ProposalForm from "#layers/ui/app/components/directory/ProposalForm.vue";
import ProposalTerms from "#layers/ui/app/components/directory/ProposalTerms.vue";

const { $t, $ts } = useI18n();

/**
 * The proposal, its versions, the agreement, and finishing the work.
 *
 * Everything shown comes from the server's record, which is the same for both
 * people; this component never decides what was agreed. What it decides is only
 * which buttons make sense to the person looking — the server refuses anything
 * out of turn regardless.
 *
 * The statement about what the server is and is not is the server's own text,
 * shown as given rather than translated here, so the words on this screen and
 * the words in an exported agreement cannot drift apart.
 */

const props = defineProps<{
  engagement: Engagement;
  /** Moves whenever the other person changed something, so it is read again. */
  revision?: number;
}>();
const emit = defineEmits<{ changed: [Engagement] }>();

const directory = useDirectory();
const negotiation = ref<Negotiation | null>(null);
const error = ref<string | null>(null);
const composing = ref(false);
const askingChange = ref(false);
const showEarlier = ref(false);
const busy = ref(false);

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

// Saying the work is complete, with a note for the other person if wanted.
// Made here, before anything is awaited, while the component can still be found.
const completion = useForm({
  defaultValues: { note: "" },
  validators: {
    onSubmit: v.object({
      note: v.pipe(v.string(), v.maxLength(500, $ts("directory.form.tooLong", { count: 500 }))),
    }),
  },
  async onSubmit({ value }) {
    await act(() => directory.requestCompletion(id.value, value.note.trim()));
  },
});

async function load() {
  try {
    negotiation.value = await directory.negotiation(id.value);
  } catch (cause) {
    error.value = isApiError(cause) ? cause.message : $ts("directory.couldNotReach");
  }
}
await load();

// The other person's changes are announced to the page, which counts them; an
// acceptance that moves nothing still moves the count. Changes made here are
// read back by act() itself.
watch(
  () => props.revision,
  () => void load(),
);

const versions = computed(() => negotiation.value?.versions ?? []);
const latest = computed<ProposalVersion | undefined>(() => versions.value.at(-1));
const earlier = computed(() => versions.value.slice(0, -1).reverse());
const previousOf = (version: ProposalVersion) =>
  versions.value.find((candidate) => candidate.version === version.version - 1);

const responsesTo = (version: number) =>
  (negotiation.value?.responses ?? []).filter((response) => response.version === version);
const acceptedLatest = computed(() =>
  latest.value
    ? responsesTo(latest.value.version)
        .filter((response) => response.kind === "accept")
        .map((response) => response.responder)
    : [],
);

const state = computed(() => props.engagement.state);
const canPropose = computed(() => ["discussion", "proposal", "negotiation"].includes(state.value));
const open = computed(() => state.value === "proposal" && latest.value);
const youAccepted = computed(() => acceptedLatest.value.includes(me.value));
const theyAccepted = computed(() => acceptedLatest.value.includes(them.value));
const sentByThem = computed(() => latest.value?.sentBy === them.value);
// While a revision is open the work cannot be recorded as complete: complete
// has to mean complete to terms both people know.
const revising = computed(() =>
  (negotiation.value?.revisions ?? []).some((revision) => revision.outcome === "open"),
);

function partLabel(part?: string) {
  if (!part) return "";
  if (part.startsWith("item:"))
    return $ts("directory.agreement.parts.item", { number: part.slice(5) });
  return $ts(`directory.agreement.parts.${part}`);
}

async function act(action: () => Promise<Engagement>) {
  if (busy.value) return;
  busy.value = true;
  error.value = null;
  try {
    emit("changed", await action());
    await load();
  } catch (cause) {
    error.value = isApiError(cause) ? cause.message : $ts("directory.couldNotReach");
  } finally {
    busy.value = false;
  }
}

function accept() {
  const version = latest.value;
  if (version) void act(() => directory.respond(id.value, version.id, "accept"));
}
function decline() {
  const version = latest.value;
  if (version) void act(() => directory.respond(id.value, version.id, "decline"));
}
function requestChange(part: string, comment: string) {
  const version = latest.value;
  askingChange.value = false;
  if (version)
    void act(() => directory.respond(id.value, version.id, "request_changes", part, comment));
}
async function sent() {
  composing.value = false;
  emit("changed", await directory.engagement(id.value));
  await load();
}

function start() {
  void act(() => directory.startWork(id.value));
}
function confirm() {
  void act(() => directory.confirmCompletion(id.value));
}

async function exportAgreement() {
  try {
    const document = await directory.exportAgreement(id.value);
    const blob = new Blob([JSON.stringify(document, null, 2)], { type: "application/json" });
    const link = window.document.createElement("a");
    link.href = URL.createObjectURL(blob);
    link.download = `agreement-${id.value}.json`;
    link.click();
    URL.revokeObjectURL(link.href);
  } catch (cause) {
    error.value = isApiError(cause) ? cause.message : $ts("directory.couldNotReach");
  }
}
</script>

<template>
  <div class="space-y-4" data-negotiation>
    <p
      v-if="error"
      class="rounded-md bg-destructive/10 px-3 py-2 text-sm text-destructive"
      role="alert"
    >
      {{ error }}
    </p>

    <!-- The agreement, once there is one: what both accepted, and when. -->
    <section
      v-if="negotiation?.agreement"
      class="space-y-3 rounded-lg border border-primary/40 p-3"
      data-agreement
    >
      <h2 class="flex items-center gap-2 text-sm font-semibold">
        <Handshake class="size-4" />
        {{ $t("directory.agreement.agreement") }}
      </h2>
      <p class="text-xs text-muted-foreground">
        {{
          negotiation.agreement.revision > 0
            ? $t("directory.revision.agreedOn", {
                number: negotiation.agreement.version,
                revision: negotiation.agreement.revision,
              })
            : $t("directory.agreement.agreedOn", { number: negotiation.agreement.version })
        }}
      </p>
      <ul class="space-y-0.5 text-xs">
        <li>
          {{
            $t("directory.agreement.clientAccepted", {
              who: props.engagement.client.username,
              when: fully(negotiation.agreement.clientAcceptedAt),
            })
          }}
        </li>
        <li>
          {{
            $t("directory.agreement.developerAccepted", {
              who: props.engagement.developer.username,
              when: fully(negotiation.agreement.developerAcceptedAt),
            })
          }}
        </li>
      </ul>
      <ProposalTerms :version="negotiation.agreement.terms" />
      <Button variant="outline" @click="exportAgreement">
        <Download class="size-3.5" />
        {{ $t("directory.agreement.export") }}
      </Button>
    </section>

    <AgreementRevisions
      v-if="negotiation?.agreement"
      :engagement="props.engagement"
      :negotiation="negotiation"
      @changed="emit('changed', $event)"
      @reload="load"
    />

    <!-- Finishing: either starts, one says it is done, the other confirms. -->
    <section
      v-if="['agreement', 'active', 'completion_pending', 'completed'].includes(state)"
      class="space-y-2"
      data-completion
    >
      <Button v-if="state === 'agreement'" :disabled="busy" @click="start">
        {{ $t("directory.agreement.start") }}
      </Button>

      <form
        v-else-if="state === 'active'"
        class="space-y-2"
        novalidate
        @submit.prevent="completion.handleSubmit()"
      >
        <completion.Field name="note">
          <template #default="{ field }">
            <Field :label="$ts('directory.agreement.completeNote', { name: them })">
              <template #default="{ id: noteId, describedBy }">
                <Textarea
                  :id="noteId"
                  rows="2"
                  :model-value="field.state.value"
                  :described-by="describedBy"
                  @update:model-value="field.handleChange($event)"
                />
              </template>
            </Field>
          </template>
        </completion.Field>
        <p v-if="revising" class="text-xs text-muted-foreground" role="status">
          {{ $t("directory.revision.settleFirst") }}
        </p>
        <Button type="submit" :disabled="busy || revising">
          {{ $t("directory.agreement.complete", { name: them }) }}
        </Button>
      </form>

      <template v-else-if="state === 'completion_pending'">
        <p
          v-if="props.engagement.completionRequestedBy === me"
          class="rounded-md bg-muted px-3 py-2 text-sm"
          role="status"
          data-completion-outstanding
        >
          {{ $t("directory.agreement.awaitingTheirs", { name: them }) }}
        </p>
        <template v-else>
          <p class="rounded-md bg-muted px-3 py-2 text-sm" role="status">
            {{ $t("directory.agreement.awaitingYours", { name: them }) }}
          </p>
          <p v-if="revising" class="text-xs text-muted-foreground" role="status">
            {{ $t("directory.revision.settleFirst") }}
          </p>
          <Button :disabled="busy || revising" @click="confirm">
            <Check class="size-3.5" />
            {{ $t("directory.agreement.confirm") }}
          </Button>
        </template>
      </template>

      <p v-else class="rounded-md bg-muted px-3 py-2 text-sm" role="status">
        {{ $t("directory.agreement.completed") }}
      </p>
    </section>

    <!-- The proposal being discussed, while nothing is agreed. -->
    <section v-if="!negotiation?.agreement" class="space-y-3">
      <template v-if="latest && !composing">
        <div class="space-y-3 rounded-lg border p-3" data-latest-proposal>
          <h2 class="text-sm font-semibold">
            {{ $t("directory.agreement.version", { number: latest.version }) }}
            <span class="font-normal text-muted-foreground">
              · {{ $t("directory.agreement.sentBy", { who: latest.sentBy }) }} ·
              <time :datetime="latest.sentAt">{{ fully(latest.sentAt) }}</time>
            </span>
          </h2>
          <ProposalTerms :version="latest" :previous="previousOf(latest)" />

          <ul v-if="responsesTo(latest.version).length" class="space-y-1 text-xs">
            <li v-for="(response, index) in responsesTo(latest.version)" :key="index">
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

          <template v-if="open">
            <p v-if="youAccepted" class="text-xs" role="status">
              {{ $t("directory.agreement.youAccepted", { name: them }) }}
            </p>
            <p v-else-if="theyAccepted" class="text-xs" role="status">
              {{ $t("directory.agreement.theyAccepted", { name: them }) }}
            </p>
            <div v-if="!youAccepted && !askingChange" class="flex flex-wrap gap-2">
              <Button :disabled="busy" @click="accept">
                <Check class="size-3.5" />
                {{ $t("directory.agreement.accept") }}
              </Button>
              <template v-if="sentByThem">
                <Button variant="outline" :disabled="busy" @click="askingChange = true">
                  {{ $t("directory.agreement.askChange") }}
                </Button>
                <Button variant="ghost" :disabled="busy" @click="decline">
                  <X class="size-3.5" />
                  {{ $t("directory.agreement.decline") }}
                </Button>
              </template>
            </div>
            <ChangeRequestForm
              v-if="askingChange"
              :version="latest"
              @send="requestChange"
              @cancel="askingChange = false"
            />
          </template>
          <p v-else-if="state === 'negotiation'" class="text-xs text-muted-foreground">
            {{ $t("directory.agreement.waiting") }}
          </p>
        </div>
      </template>

      <div
        v-else-if="!composing"
        class="rounded-lg border border-dashed px-3 py-6 text-center text-sm"
      >
        <p class="font-medium">{{ $t("directory.agreement.noProposal") }}</p>
        <p class="mt-1 text-xs text-muted-foreground">
          {{ $t("directory.agreement.noProposalHint") }}
        </p>
      </div>

      <ProposalForm
        v-if="composing"
        :engagement-id="id"
        :from="latest"
        @sent="sent"
        @cancel="composing = false"
      />
      <Button v-else-if="canPropose" variant="outline" @click="composing = true">
        <FilePen class="size-3.5" />
        {{ latest ? $t("directory.agreement.revise") : $t("directory.agreement.write") }}
      </Button>

      <div v-if="earlier.length">
        <button
          type="button"
          class="flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
          :aria-expanded="showEarlier"
          @click="showEarlier = !showEarlier"
        >
          {{ $t("directory.agreement.earlier") }}
          <ChevronDown class="size-3.5 transition-transform" :class="showEarlier && 'rotate-180'" />
        </button>
        <ol v-if="showEarlier" class="mt-2 space-y-3">
          <li
            v-for="version in earlier"
            :key="version.id"
            class="space-y-2 rounded-lg border p-3 opacity-80"
          >
            <h3 class="text-xs font-semibold">
              {{ $t("directory.agreement.version", { number: version.version }) }}
              <span class="font-normal text-muted-foreground">
                · {{ $t("directory.agreement.sentBy", { who: version.sentBy }) }}
              </span>
            </h3>
            <ProposalTerms :version="version" :previous="previousOf(version)" />
            <ul class="space-y-1 text-xs">
              <li v-for="(response, index) in responsesTo(version.version)" :key="index">
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
          </li>
        </ol>
      </div>
    </section>

    <!-- What the server is, in its own words, wherever an agreement is made
         or shown. -->
    <aside
      v-if="negotiation && (latest || negotiation.agreement)"
      class="flex gap-2 rounded-md border bg-muted/40 px-3 py-2 text-xs"
      :aria-label="$ts('directory.agreement.disclosureTitle')"
    >
      <Info class="mt-0.5 size-3.5 shrink-0" />
      <p data-disclosure>{{ negotiation.disclosure }}</p>
    </aside>
  </div>
</template>
