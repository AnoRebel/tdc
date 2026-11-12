<script setup lang="ts">
import type { DirectoryVisibility } from "@tdc/types";
import { EyeOff, Globe, Users } from "lucide-vue-next";

const { $t } = useI18n();

/**
 * Who may see something: a choice of three, each saying what it means.
 *
 * Radio buttons rather than a select, because the difference between the three
 * is the whole decision and a closed dropdown hides two of the explanations.
 * A fieldset with a legend, so a screen reader announces what is being decided
 * before the options — "Contact details: visible to members" rather than a bare
 * "members".
 */

const props = defineProps<{
  legend: string;
  hint?: string;
  /** A company cannot be hidden, so it offers two. */
  choices?: DirectoryVisibility[];
}>();

const model = defineModel<DirectoryVisibility>({ required: true });

const name = useId();

const all = computed(() => [
  {
    value: "public" as const,
    icon: Globe,
    label: $t("directory.visibility.public"),
    detail: $t("directory.visibility.publicHint"),
  },
  {
    value: "members" as const,
    icon: Users,
    label: $t("directory.visibility.members"),
    detail: $t("directory.visibility.membersHint"),
  },
  {
    value: "hidden" as const,
    icon: EyeOff,
    label: $t("directory.visibility.hidden"),
    detail: $t("directory.visibility.hiddenHint"),
  },
]);

const offered = computed(() =>
  all.value.filter((option) => !props.choices || props.choices.includes(option.value)),
);
</script>

<template>
  <fieldset class="space-y-2">
    <legend class="text-sm font-medium">{{ props.legend }}</legend>
    <p v-if="props.hint" class="text-xs text-muted-foreground">{{ props.hint }}</p>

    <div class="grid gap-2 sm:grid-cols-3">
      <label
        v-for="option in offered"
        :key="option.value"
        class="flex cursor-pointer gap-2.5 rounded-lg border p-3 text-sm transition-colors has-checked:border-primary has-checked:bg-primary/5 has-focus-visible:ring-2 has-focus-visible:ring-ring"
      >
        <input v-model="model" type="radio" class="sr-only" :name="name" :value="option.value" />
        <component :is="option.icon" class="mt-0.5 size-4 shrink-0 text-muted-foreground" />
        <span class="min-w-0">
          <span class="block font-medium">{{ option.label }}</span>
          <span class="mt-0.5 block text-xs text-muted-foreground">{{ option.detail }}</span>
        </span>
      </label>
    </div>
  </fieldset>
</template>
