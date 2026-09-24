<script setup lang="ts">
import type { ProfileSkill, Skill } from "@tdc/types";
import { isApiError } from "@tdc/types";
import { watchDebounced } from "@vueuse/core";
import { Clock, Plus, X } from "lucide-vue-next";
import { Button } from "#layers/ui/app/components/ui/button";
import { Input } from "#layers/ui/app/components/ui/input";

const { $t, $ts } = useI18n();

/**
 * Choosing skills from the vocabulary.
 *
 * A search over the vocabulary rather than free text, because free text is
 * how one skill becomes five spellings nobody can search for. What somebody
 * types is matched against names and other spellings; when nothing matches,
 * they may propose it, and it waits for review — on their own profile, marked
 * as waiting, and findable by nobody until a reviewer agrees it exists.
 */

const model = defineModel<ProfileSkill[]>({ required: true });

const directory = useDirectory();
const inputId = useId();
const query = ref("");
const suggestions = ref<Skill[]>([]);
const error = ref<string | null>(null);
const proposing = ref(false);

watchDebounced(
  query,
  async (typed) => {
    if (!typed.trim()) {
      suggestions.value = [];
      return;
    }
    try {
      suggestions.value = await directory.skills(typed);
    } catch {
      suggestions.value = [];
    }
  },
  { debounce: 250 },
);

const chosen = computed(() => new Set(model.value.map((skill) => skill.id)));
const offered = computed(() =>
  suggestions.value.filter((skill) => !chosen.value.has(skill.id)).slice(0, 8),
);
const exactMatch = computed(() =>
  suggestions.value.some((skill) => skill.name.toLowerCase() === query.value.trim().toLowerCase()),
);

function add(skill: Skill) {
  if (chosen.value.has(skill.id)) return;
  model.value = [
    ...model.value,
    { id: skill.id, name: skill.name, status: skill.status, description: "" },
  ];
  query.value = "";
  suggestions.value = [];
}

function remove(index: number) {
  model.value = model.value.filter((_, at) => at !== index);
}

function describe(index: number, description: string) {
  model.value = model.value.map((skill, at) => (at === index ? { ...skill, description } : skill));
}

async function propose() {
  error.value = null;
  proposing.value = true;
  try {
    add(await directory.proposeSkill(query.value.trim()));
  } catch (cause) {
    error.value = isApiError(cause) ? cause.message : $ts("directory.couldNotReach");
  } finally {
    proposing.value = false;
  }
}
</script>

<template>
  <fieldset class="space-y-3">
    <legend class="text-sm font-medium">{{ $t("directory.skills.title") }}</legend>
    <p class="text-xs text-muted-foreground">{{ $t("directory.skills.hint") }}</p>

    <ul v-if="model.length" class="space-y-2">
      <li
        v-for="(skill, index) in model"
        :key="skill.id ?? skill.name"
        class="flex flex-wrap items-center gap-2 rounded-lg border p-2"
      >
        <span class="text-sm font-medium">{{ skill.name }}</span>
        <span
          v-if="skill.status === 'proposed'"
          class="flex items-center gap-1 rounded-full bg-warning/15 px-2 py-0.5 text-[0.6875rem] text-foreground"
        >
          <Clock class="size-3" />
          {{ $t("directory.skills.waiting") }}
        </span>
        <Input
          class="h-8 min-w-40 flex-1"
          :model-value="skill.description"
          :label="$ts('directory.skills.describeWhich', { skill: skill.name })"
          :placeholder="$ts('directory.skills.describe')"
          @update:model-value="describe(index, $event)"
        />
        <Button
          variant="ghost"
          size="icon"
          :label="$ts('directory.skills.remove', { skill: skill.name })"
          @click="remove(index)"
        >
          <X class="size-3.5" />
        </Button>
      </li>
    </ul>

    <div>
      <label :for="inputId" class="sr-only">{{ $t("directory.skills.find") }}</label>
      <Input
        :id="inputId"
        v-model="query"
        autocomplete="off"
        :placeholder="$ts('directory.skills.find')"
      />
      <p class="sr-only" aria-live="polite">
        {{ query.trim() ? $t("directory.skills.matches", { count: offered.length }) : "" }}
      </p>

      <ul v-if="offered.length" class="mt-2 flex flex-wrap gap-2">
        <li v-for="skill in offered" :key="skill.id">
          <button
            type="button"
            class="flex items-center gap-1 rounded-full border px-3 py-1 text-xs hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
            @click="add(skill)"
          >
            <Plus class="size-3" />
            {{ skill.name }}
            <span v-if="skill.status === 'proposed'" class="text-muted-foreground">
              · {{ $t("directory.skills.waiting") }}
            </span>
          </button>
        </li>
      </ul>

      <div v-if="query.trim() && !exactMatch" class="mt-2 flex flex-wrap items-center gap-2">
        <p class="text-xs text-muted-foreground">
          {{ $t("directory.skills.notInVocabulary", { skill: query.trim() }) }}
        </p>
        <Button variant="outline" size="small" :disabled="proposing" @click="propose">
          {{ $t("directory.skills.propose") }}
        </Button>
      </div>
      <p v-if="error" role="alert" class="mt-1 text-xs text-destructive">{{ error }}</p>
    </div>
  </fieldset>
</template>
