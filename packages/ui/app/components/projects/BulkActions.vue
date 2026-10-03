<script setup lang="ts">
import { isApiError, type BulkChange, type BulkResult, type Project } from "@tdc/types";
import { Button } from "#layers/ui/app/components/ui/button";
import { Field } from "#layers/ui/app/components/ui/field";
import { Input } from "#layers/ui/app/components/ui/input";
import { Select } from "#layers/ui/app/components/ui/select";

const { $t, $ts, $localeRoute: localeRoute } = useI18n();

/**
 * One change to every selected issue, and what became of each.
 *
 * The report is the point. Each issue is decided on its own — somebody may
 * label the issues they wrote and not the rest — so a batch can come back
 * half done, and the only honest answer is a line per issue: changed, already
 * so, refused and by which permission, or not there.
 */
const props = defineProps<{ project: Project; selected: string[] }>();
const emit = defineEmits<{ applied: [results: BulkResult[]]; clear: [] }>();

const projectsApi = useProjects();
const uid = useId();

const kind = ref<BulkChange["kind"]>("label");
const value = ref("");
const person = ref("");
const busy = ref(false);
const failed = ref<string | null>(null);
const results = ref<BulkResult[] | null>(null);

const kindOptions = computed(() =>
  (["label", "unlabel", "state", "priority", "assign", "unassign"] as const).map((k) => ({
    value: k,
    label: $ts(`projects.bulk.kinds.${k}`),
  })),
);

/** What the chosen change can name, for the changes that name a thing. */
const valueOptions = computed(() => {
  switch (kind.value) {
    case "label":
    case "unlabel":
      return (props.project.labels ?? []).map((label) => ({ value: label.id, label: label.name }));
    case "state":
      return (props.project.states ?? []).map((state) => ({ value: state.id, label: state.name }));
    case "priority":
      return [
        { value: "none", label: $ts("projects.priority.noneOption") },
        ...(props.project.priorities ?? []).map((level) => ({
          value: level.id,
          label: level.name,
        })),
      ];
    default:
      return [];
  }
});
const namesPerson = computed(() => kind.value === "assign" || kind.value === "unassign");

watch(kind, () => {
  value.value = valueOptions.value[0]?.value ?? "";
});
watch(
  valueOptions,
  (options) => {
    if (!options.some((option) => option.value === value.value))
      value.value = options[0]?.value ?? "";
  },
  { immediate: true },
);

const ready = computed(
  () =>
    props.selected.length > 0 &&
    !busy.value &&
    (namesPerson.value ? person.value.trim() !== "" : value.value !== ""),
);

async function apply() {
  if (!ready.value) return;
  busy.value = true;
  failed.value = null;
  try {
    const change: BulkChange = {
      kind: kind.value,
      value: namesPerson.value
        ? person.value.trim()
        : kind.value === "priority" && value.value === "none"
          ? ""
          : value.value,
    };
    const answer = await projectsApi.bulk(props.project.key, props.selected, change);
    results.value = answer.results;
    emit("applied", answer.results);
  } catch (cause) {
    failed.value = isApiError(cause) ? cause.message : $ts("projects.bulk.failed");
  } finally {
    busy.value = false;
  }
}

const counts = computed(() => {
  const tally = { changed: 0, other: 0 };
  for (const result of results.value ?? []) {
    if (result.changed) tally.changed++;
    else tally.other++;
  }
  return tally;
});

const icons: Record<BulkResult["code"], string> = {
  changed: "lucide:circle-check",
  unchanged: "lucide:circle-minus",
  refused: "lucide:ban",
  not_found: "lucide:circle-help",
  failed: "lucide:circle-alert",
};
</script>

<template>
  <section class="space-y-3 rounded-xl border bg-card p-3" :aria-label="$ts('projects.bulk.title')">
    <div class="flex flex-wrap items-end gap-3">
      <p class="text-sm font-medium" aria-live="polite">
        {{ $t("projects.bulk.selected", { count: props.selected.length }) }}
      </p>
      <Field :label="$ts('projects.bulk.change')" class="w-44">
        <template #default="{ id }">
          <Select
            :id="id"
            v-model="kind"
            :label="$ts('projects.bulk.change')"
            :options="kindOptions"
          />
        </template>
      </Field>
      <Field v-if="namesPerson" :label="$ts('projects.bulk.person')" class="w-44">
        <template #default="{ id }">
          <Input :id="id" v-model="person" :placeholder="$ts('projects.bulk.personPlaceholder')" />
        </template>
      </Field>
      <Field v-else :label="$ts('projects.bulk.to')" class="w-44">
        <template #default="{ id }">
          <Select
            :id="id"
            v-model="value"
            :label="$ts('projects.bulk.to')"
            :options="valueOptions"
          />
        </template>
      </Field>
      <Button :disabled="!ready" @click="apply">
        <Icon v-if="busy" name="lucide:loader-circle" class="size-3.5 animate-spin" />
        {{ $t("projects.bulk.apply") }}
      </Button>
      <Button variant="ghost" @click="((results = null), emit('clear'))">
        {{ $t("projects.bulk.clear") }}
      </Button>
    </div>

    <p v-if="failed" class="text-sm text-destructive" role="alert">{{ failed }}</p>

    <div v-if="results" :id="`${uid}-report`" role="status">
      <p class="mb-2 text-sm">
        {{ $t("projects.bulk.outcome", { changed: counts.changed, other: counts.other }) }}
      </p>
      <ul class="divide-y rounded-lg border bg-surface" :aria-label="$ts('projects.bulk.report')">
        <li
          v-for="result in results"
          :key="result.issue"
          class="flex items-start gap-2 px-3 py-1.5 text-sm"
          :data-outcome="result.code"
        >
          <Icon
            :name="icons[result.code]"
            class="mt-0.5 size-3.5 shrink-0"
            :class="result.changed ? 'text-success-text' : 'text-muted-foreground'"
            aria-hidden="true"
          />
          <NuxtLink
            v-if="result.code !== 'not_found'"
            :to="localeRoute({ name: 'issues-ref', params: { ref: result.issue } })"
            class="font-mono text-xs hover:underline"
          >
            {{ result.issue }}
          </NuxtLink>
          <span v-else class="font-mono text-xs">{{ result.issue }}</span>
          <span class="font-medium">{{ $t(`projects.bulk.codes.${result.code}`) }}</span>
          <span v-if="!result.changed && result.reason" class="text-muted-foreground">
            — {{ result.reason }}
          </span>
        </li>
      </ul>
    </div>
  </section>
</template>
