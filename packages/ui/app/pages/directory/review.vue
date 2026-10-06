<script setup lang="ts">
import type { Claim, ProfileClaims, QueuedSkill, Skill } from "@tdc/types";
import { isApiError } from "@tdc/types";
import { useForm } from "@tanstack/vue-form";
import * as v from "valibot";
import { ArrowLeft, BadgeCheck, Check, GitMerge, Tags, X } from "lucide-vue-next";
import { Field } from "#layers/ui/app/components/ui/field";
import { Button } from "#layers/ui/app/components/ui/button";
import { Input } from "#layers/ui/app/components/ui/input";
import { Textarea } from "#layers/ui/app/components/ui/textarea";
import { onDate } from "#layers/ui/app/lib/dates";

const { $t, $ts, $localeRoute: localeRoute } = useI18n();

/**
 * The directory's review queue.
 *
 * Proposed skills wait here for somebody who manages the vocabulary. Three
 * answers, each recorded: yes, it is a skill (approve); no, it is not
 * (reject, with a reason); or it is a skill we already have under another name
 * (merge), which is the answer that keeps search reliable — the proposal's
 * spelling becomes another way of naming the one entry.
 */

definePageMeta({ middleware: "auth", layout: "app" });

useHead({ title: $ts("directory.review.title") });

const directory = useDirectory();
const { can } = useAuth();

const queue = ref<QueuedSkill[]>([]);
const error = ref<string | null>(null);
const refused = ref(false);

async function load() {
  error.value = null;
  try {
    queue.value = await directory.skillQueue();
  } catch (cause) {
    if (isApiError(cause) && cause.code === "forbidden") refused.value = true;
    else error.value = isApiError(cause) ? cause.message : $ts("directory.couldNotReach");
  }
}

onMounted(() => {
  if (can("directory.skills.manage")) void load();
});

async function act(action: () => Promise<unknown>) {
  error.value = null;
  try {
    await action();
    await load();
  } catch (cause) {
    error.value = isApiError(cause) ? cause.message : $ts("directory.couldNotReach");
  }
}

// Rejecting asks why: the reason is kept with the record.
const rejecting = ref<QueuedSkill | null>(null);

const rejection = useForm({
  defaultValues: { reason: "" },
  validators: { onSubmit: v.object({ reason: v.pipe(v.string(), v.trim(), v.minLength(1)) }) },
  async onSubmit({ value, formApi }) {
    const skill = rejecting.value;
    if (!skill) return;
    rejecting.value = null;
    await act(() => directory.rejectSkill(skill.id, value.reason.trim()));
    formApi.reset();
  },
});

// Merging asks into what, from the vocabulary.
const merging = ref<QueuedSkill | null>(null);
const target = ref("");
const targets = ref<Skill[]>([]);
watchDebounced(
  target,
  async (typed: string) => {
    targets.value = typed.trim()
      ? (await directory.skills(typed)).filter((skill) => skill.status === "active")
      : [];
  },
  { debounce: 250 },
);

/*
 * Verifying claims on one profile.
 *
 * A verifier looks a profile up, reads each claim as it stands, and attests to
 * one at a time with a note on what they checked — or withdraws an attestation
 * with a reason. There is no action for the profile as a whole, because there
 * is no such verification.
 */
const profileClaims = ref<ProfileClaims | null>(null);
const notes = ref<Record<string, string>>({});
const reasons = ref<Record<string, string>>({});

const lookup = useForm({
  defaultValues: { username: "" },
  validators: { onSubmit: v.object({ username: v.pipe(v.string(), v.trim(), v.minLength(1)) }) },
  async onSubmit({ value }) {
    await loadClaims(value.username.trim());
  },
});

async function loadClaims(username: string) {
  error.value = null;
  try {
    profileClaims.value = await directory.claims(username);
  } catch (cause) {
    profileClaims.value = null;
    error.value = isApiError(cause) ? cause.message : $ts("directory.couldNotReach");
  }
}

async function grant(claim: Claim) {
  const profile = profileClaims.value;
  if (!profile) return;
  await act(() =>
    directory.verifyClaim(profile.username, claim.kind, claim.id, notes.value[claim.id] ?? ""),
  );
  notes.value[claim.id] = "";
  await loadClaims(profile.username);
}

async function revoke(claim: Claim) {
  const profile = profileClaims.value;
  if (!profile || !claim.verification) return;
  const id = claim.verification.id;
  await act(() => directory.revokeVerification(id, reasons.value[claim.id] ?? ""));
  reasons.value[claim.id] = "";
  await loadClaims(profile.username);
}

async function mergeInto(skill: Skill) {
  const proposal = merging.value;
  if (!proposal) return;
  merging.value = null;
  target.value = "";
  await act(() => directory.mergeSkill(proposal.id, skill.id));
}
</script>

<template>
  <div class="flex-1 overflow-y-auto">
    <header class="flex h-12 items-center gap-3 border-b px-4">
      <NuxtLink
        :to="localeRoute({ name: 'directory' })"
        class="btn btn-ghost size-7 px-0"
        :aria-label="$ts('directory.backToDirectory')"
      >
        <ArrowLeft class="size-4" />
      </NuxtLink>
      <h1 class="text-sm font-semibold">{{ $t("directory.review.title") }}</h1>
    </header>

    <div class="max-w-3xl space-y-8 px-4 py-6">
      <p
        v-if="error"
        class="rounded-md bg-destructive/10 px-3 py-2 text-sm text-destructive"
        role="alert"
      >
        {{ error }}
      </p>

      <section v-if="can('directory.skills.manage')" class="space-y-3">
        <h2 class="flex items-center gap-2 text-sm font-semibold">
          <Tags class="size-4 text-muted-foreground" />
          {{ $t("directory.review.skills") }}
        </h2>
        <p class="text-xs text-muted-foreground">{{ $t("directory.review.skillsHint") }}</p>

        <p
          v-if="refused"
          class="rounded-md bg-destructive/10 px-3 py-2 text-sm text-destructive"
          role="alert"
        >
          {{ $t("directory.review.refused") }}
        </p>
        <div v-else-if="!queue.length" class="rounded-lg border border-dashed py-8 text-center">
          <p class="text-sm font-medium">{{ $t("directory.review.nothingWaiting") }}</p>
        </div>

        <ul class="space-y-2">
          <li v-for="skill in queue" :key="skill.id" class="rounded-lg border p-3">
            <p class="text-sm font-medium">{{ skill.name }}</p>
            <p class="text-xs text-muted-foreground">
              {{
                $t("directory.review.proposedBy", {
                  who: skill.proposedBy || "—",
                  date: onDate(skill.createdAt),
                  count: skill.profiles,
                })
              }}
            </p>
            <p v-if="skill.description" class="mt-1 text-sm">{{ skill.description }}</p>

            <div class="mt-3 flex flex-wrap gap-2">
              <Button variant="outline" @click="act(() => directory.approveSkill(skill.id))">
                <Check class="size-3.5" />
                {{ $t("directory.review.approve") }}
              </Button>
              <Button variant="ghost" @click="merging = merging === skill ? null : skill">
                <GitMerge class="size-3.5" />
                {{ $t("directory.review.merge") }}
              </Button>
              <Button variant="ghost" @click="rejecting = skill">
                <X class="size-3.5" />
                {{ $t("directory.review.reject") }}
              </Button>
            </div>

            <div v-if="merging === skill" class="mt-3 space-y-2">
              <Input
                v-model="target"
                :label="$ts('directory.review.mergeInto', { skill: skill.name })"
                :placeholder="$ts('directory.review.mergeInto', { skill: skill.name })"
              />
              <ul class="flex flex-wrap gap-2">
                <li v-for="candidate in targets" :key="candidate.id">
                  <Button variant="outline" size="small" @click="mergeInto(candidate)">
                    {{ candidate.name }}
                  </Button>
                </li>
              </ul>
            </div>
          </li>
        </ul>
      </section>

      <section v-if="can('directory.verify')" class="space-y-3">
        <h2 class="flex items-center gap-2 text-sm font-semibold">
          <BadgeCheck class="size-4 text-muted-foreground" />
          {{ $t("directory.verification.title") }}
        </h2>
        <p class="text-xs text-muted-foreground">{{ $t("directory.verification.reviewHint") }}</p>

        <form class="flex flex-wrap items-end gap-2" @submit.prevent="lookup.handleSubmit()">
          <lookup.Field name="username">
            <template #default="{ field }">
              <Field :label="$ts('auth.username')" class="max-w-xs">
                <template #default="{ id }">
                  <Input
                    :id="id"
                    :model-value="field.state.value"
                    autocomplete="off"
                    @update:model-value="field.handleChange($event)"
                  />
                </template>
              </Field>
            </template>
          </lookup.Field>
          <Button type="submit" class="h-9">{{ $t("directory.verification.open") }}</Button>
        </form>

        <ul v-if="profileClaims" class="space-y-2">
          <li
            v-for="claim in profileClaims.claims"
            :key="claim.kind + claim.id"
            class="space-y-2 rounded-lg border p-3"
          >
            <p class="flex flex-wrap items-center gap-2 text-sm">
              <span class="rounded bg-muted px-1.5 py-0.5 text-[0.6875rem] text-muted-foreground">
                {{ $t(`directory.verification.kinds.${claim.kind}`) }}
              </span>
              <span class="font-medium">{{ claim.label }}</span>
              <DirectoryVerifiedBadge v-if="claim.verification" :verified="claim.verification" />
            </p>
            <dl class="grid grid-cols-[auto_1fr] gap-x-3 gap-y-0.5 text-xs">
              <template v-for="(value, key) in claim.content" :key="key">
                <dt class="text-muted-foreground">{{ key }}</dt>
                <dd class="break-words">{{ value || "—" }}</dd>
              </template>
            </dl>

            <div v-if="!claim.verification" class="flex flex-wrap items-center gap-2">
              <Input
                v-model="notes[claim.id]"
                class="min-w-48 flex-1"
                :label="$ts('directory.verification.whatYouChecked', { claim: claim.label })"
                :placeholder="$ts('directory.verification.whatYouCheckedHint')"
              />
              <Button variant="outline" :disabled="!notes[claim.id]?.trim()" @click="grant(claim)">
                {{ $t("directory.verification.verify") }}
              </Button>
            </div>
            <div v-else class="flex flex-wrap items-center gap-2">
              <Input
                v-model="reasons[claim.id]"
                class="min-w-48 flex-1"
                :label="$ts('directory.verification.whyRevoke', { claim: claim.label })"
                :placeholder="$ts('directory.verification.whyRevokeHint')"
              />
              <Button
                variant="danger"
                :disabled="!reasons[claim.id]?.trim()"
                @click="revoke(claim)"
              >
                {{ $t("directory.verification.revoke") }}
              </Button>
            </div>
          </li>
        </ul>
      </section>
    </div>

    <!-- eslint-disable-next-line vuejs-accessibility/no-static-element-interactions -- a dialog backdrop, dismissed by Escape as well -->
    <div
      v-if="rejecting"
      class="fixed inset-0 z-50 flex items-center justify-center bg-background/70 p-4 backdrop-blur-sm"
      role="dialog"
      aria-modal="true"
      :aria-label="$ts('directory.review.rejectTitle', { skill: rejecting.name })"
      @click.self="rejecting = null"
      @keydown.esc="rejecting = null"
    >
      <form
        class="w-[min(26rem,100%)] space-y-3 rounded-xl border bg-card p-4 shadow-2xl"
        @submit.prevent="rejection.handleSubmit()"
      >
        <h2 class="text-sm font-semibold">
          {{ $t("directory.review.rejectTitle", { skill: rejecting.name }) }}
        </h2>
        <label class="sr-only" for="reject-reason">{{ $t("directory.review.why") }}</label>
        <rejection.Field name="reason">
          <template #default="{ field }">
            <Textarea
              id="reject-reason"
              rows="3"
              :model-value="field.state.value"
              :placeholder="$ts('directory.review.why')"
              @update:model-value="field.handleChange($event)"
            />
            <div class="mt-3 flex justify-end gap-2">
              <Button variant="ghost" @click="rejecting = null">{{ $t("action.cancel") }}</Button>
              <Button type="submit" :disabled="!field.state.value.trim()">
                {{ $t("directory.review.reject") }}
              </Button>
            </div>
          </template>
        </rejection.Field>
      </form>
    </div>
  </div>
</template>
