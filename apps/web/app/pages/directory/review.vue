<script setup lang="ts">
import type { QueuedSkill, Skill } from "@tdc/types";
import { isApiError } from "@tdc/types";
import { useForm } from "@tanstack/vue-form";
import * as v from "valibot";
import { ArrowLeft, Check, GitMerge, Tags, X } from "lucide-vue-next";
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

onMounted(load);

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
