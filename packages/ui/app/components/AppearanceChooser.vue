<script setup lang="ts">
import { Contrast, Monitor, Moon, Sun } from "lucide-vue-next";

import type { ContrastPreference } from "#layers/ui/app/lib/interface";
import { loadMaterials } from "#layers/ui/app/lib/materials";
import type { Material, Palette } from "#layers/ui/app/lib/theme";
import { Button } from "#layers/ui/app/components/ui/button";
import { Input } from "#layers/ui/app/components/ui/input";
import { Switch } from "#layers/ui/app/components/ui/switch";

/**
 * Everything about how the interface looks, in one place.
 *
 * Light or dark, high contrast, the palette or one of Crystal Neuo's six
 * materials, frosted glass, and an accent. Shared by the settings page, the
 * desktop client's appearance window and the appearance dialog, so all three
 * offer the same choices and none can fall behind.
 *
 * Each material is shown rather than described: its card is drawn in the
 * material itself, in whichever of light and dark the page is in, so somebody
 * choosing sees what they will get. That needs the materials' stylesheet,
 * which is fetched when this first appears rather than with the application.
 */

const { $t, $ts } = useI18n();
const theme = useTheme();
const { contrastPreference, glass } = useInterfacePreferences();

onMounted(() => void loadMaterials());

const name = useId();
const modeHeading = useId();
const contrastHeading = useId();

const modes = computed(() => [
  { value: "auto" as const, label: $ts("settings.followMyDevice"), icon: Monitor },
  { value: "light" as const, label: $ts("settings.light"), icon: Sun },
  { value: "dark" as const, label: $ts("settings.dark"), icon: Moon },
]);

const contrasts = computed(() => [
  { value: "standard" as ContrastPreference, label: $ts("appearance.contrast.standard") },
  { value: "more" as ContrastPreference, label: $ts("appearance.contrast.more") },
  { value: "system" as ContrastPreference, label: $ts("settings.followMyDevice") },
]);

const palettes = computed(() => [
  {
    value: "default" as Palette,
    label: $ts("settings.paletteCool"),
    description: $ts("settings.paletteCoolDescription"),
    swatches: ["oklch(0.985 0.002 265)", "oklch(0.53 0.16 269)", "oklch(0.21 0.012 265)"],
  },
  {
    value: "congrator" as Palette,
    label: $ts("settings.paletteWarm"),
    description: $ts("settings.paletteWarmDescription"),
    swatches: ["oklch(0.991 0.005 95)", "oklch(0.449 0.15 35)", "oklch(0.151 0.02 20)"],
  },
]);

const materials = computed(() =>
  (["refraction", "tactile", "signal", "folio", "bloom", "hybrid"] as Material[]).map((value) => ({
    value,
    label: $ts(`appearance.material.${value}.name`),
    description: $ts(`appearance.material.${value}.description`),
  })),
);

/** Whether frosted glass means anything for what is chosen: three materials are glass. */
const glassy = computed(() => ["refraction", "bloom", "hybrid"].includes(theme.palette.value));

/*
 * The accent, typed rather than picked from a list. Whatever is chosen becomes
 * two shades of itself, one per appearance, so its label is readable in both.
 */
const accentDraft = ref("");
const accentRefusal = ref("");

watchEffect(() => {
  accentDraft.value = theme.accent.value;
});

function applyAccent() {
  const { refused } = theme.setAccent(accentDraft.value);
  accentRefusal.value = refused ? $ts(`settings.accentRefused.${refused}`) : "";
}

function clearAccent() {
  accentDraft.value = "";
  accentRefusal.value = "";
  theme.setAccent("");
}
</script>

<template>
  <div class="space-y-5">
    <div>
      <h3 :id="modeHeading" class="text-xs font-semibold">{{ $t("appearance.lightOrDark") }}</h3>
      <div class="mt-2 flex flex-wrap gap-1.5" role="group" :aria-labelledby="modeHeading">
        <Button
          v-for="option in modes"
          :key="option.value"
          :variant="theme.preference.value === option.value ? 'primary' : 'outline'"
          :aria-pressed="theme.preference.value === option.value"
          @click="theme.set(option.value)"
        >
          <component :is="option.icon" class="size-3.5" aria-hidden="true" />
          {{ option.label }}
        </Button>
      </div>
    </div>

    <div>
      <h3 :id="contrastHeading" class="flex items-center gap-1.5 text-xs font-semibold">
        <Contrast class="size-3.5" aria-hidden="true" />
        {{ $t("appearance.contrast.title") }}
      </h3>
      <p class="mt-1 text-xs text-muted-foreground">{{ $t("appearance.contrast.hint") }}</p>
      <div class="mt-2 flex flex-wrap gap-1.5" role="group" :aria-labelledby="contrastHeading">
        <Button
          v-for="option in contrasts"
          :key="option.value"
          :variant="contrastPreference === option.value ? 'primary' : 'outline'"
          :aria-pressed="contrastPreference === option.value"
          @click="contrastPreference = option.value"
        >
          {{ option.label }}
        </Button>
      </div>
    </div>

    <!-- One choice: a palette or a material, never both, so one group of
         radio buttons across the two lists. Arrow keys move through all
         eight, as they would through any group. -->
    <fieldset>
      <legend class="text-xs font-semibold">{{ $t("settings.colours") }}</legend>
      <p class="mt-1 text-xs text-muted-foreground">{{ $t("settings.coloursHint") }}</p>

      <div class="mt-2 grid grid-cols-2 gap-2">
        <label
          v-for="option in palettes"
          :key="option.value"
          class="relative flex cursor-pointer items-center gap-2.5 rounded-lg border p-2.5 transition-colors has-checked:border-primary has-checked:bg-primary/5 has-focus-visible:ring-2 has-focus-visible:ring-ring"
        >
          <input
            :checked="theme.palette.value === option.value"
            type="radio"
            class="sr-only"
            :name="name"
            :value="option.value"
            @change="theme.setPalette(option.value)"
          />
          <span class="flex shrink-0 -space-x-1" aria-hidden="true">
            <span
              v-for="swatch in option.swatches"
              :key="swatch"
              class="size-4 rounded-full border border-border"
              :style="{ background: swatch }"
            />
          </span>
          <span class="min-w-0">
            <span class="block text-xs font-medium">{{ option.label }}</span>
            <span class="block truncate text-[0.6875rem] text-muted-foreground">{{
              option.description
            }}</span>
          </span>
        </label>
      </div>

      <h4 class="mt-4 text-xs font-semibold">{{ $t("appearance.materials") }}</h4>
      <p class="mt-1 text-xs text-muted-foreground">
        {{ $t("appearance.materialsHint") }}
      </p>

      <div class="mt-2 grid grid-cols-2 gap-2 sm:grid-cols-3">
        <label
          v-for="option in materials"
          :key="option.value"
          class="group relative cursor-pointer rounded-xl border p-1.5 transition-colors has-checked:border-primary has-checked:bg-primary/5 has-focus-visible:ring-2 has-focus-visible:ring-ring"
        >
          <input
            :checked="theme.palette.value === option.value"
            type="radio"
            class="sr-only"
            :name="name"
            :value="option.value"
            @change="theme.setPalette(option.value)"
          />
          <!-- The material itself, at the size of a thumbnail. Decorative: its
               name and description below are what is read out. -->
          <span
            :data-theme="option.value"
            class="material-preview flex h-24 overflow-hidden rounded-lg border bg-background text-foreground"
            aria-hidden="true"
          >
            <span data-chrome class="w-7 shrink-0 border-r bg-surface-sunken" />
            <span class="flex min-w-0 flex-1 flex-col gap-1.5 p-2">
              <span data-display class="text-sm leading-none font-semibold">Aa</span>
              <span class="h-1.5 w-4/5 rounded-full bg-muted-foreground/40" />
              <span class="h-1.5 w-3/5 rounded-full bg-muted-foreground/25" />
              <span class="mt-auto flex gap-1">
                <span
                  data-control
                  data-variant="primary"
                  class="h-4 w-9 rounded-md bg-primary shadow-xs"
                />
                <span
                  data-control
                  data-variant="outline"
                  class="h-4 w-7 rounded-md border bg-surface"
                />
              </span>
            </span>
          </span>
          <span class="block px-1 pt-1.5 text-xs font-medium">{{ option.label }}</span>
          <span class="block px-1 pb-0.5 text-[0.6875rem] leading-snug text-muted-foreground">
            {{ option.description }}
          </span>
        </label>
      </div>
    </fieldset>

    <div v-if="glassy" class="flex items-start gap-2.5">
      <Switch v-model="glass" :label="$ts('appearance.glass.label')" class="mt-0.5" />
      <div>
        <p class="text-xs">{{ $t("appearance.glass.label") }}</p>
        <p class="text-xs text-muted-foreground">{{ $t("appearance.glass.hint") }}</p>
      </div>
    </div>

    <div>
      <h3 class="text-xs font-semibold">{{ $t("settings.accent") }}</h3>
      <p class="mt-1 text-xs text-muted-foreground">{{ $t("settings.accentHint") }}</p>
      <div class="mt-2 flex flex-wrap items-start gap-1.5">
        <label class="sr-only" :for="`${name}-accent`">{{ $t("settings.accentColour") }}</label>
        <Input
          :id="`${name}-accent`"
          v-model="accentDraft"
          class="w-40"
          placeholder="#4f46e5"
          :aria-invalid="Boolean(accentRefusal)"
          :aria-describedby="accentRefusal ? `${name}-accent-refusal` : undefined"
          @keydown.enter.prevent="applyAccent"
        />
        <Button variant="outline" @click="applyAccent">{{ $t("settings.apply") }}</Button>
        <Button v-if="theme.accent.value" variant="ghost" @click="clearAccent">
          {{ $t("settings.reset") }}
        </Button>
      </div>
      <!-- The number, not just "that will not work": somebody who is short of
           readable should be told how far off they are. -->
      <p
        v-if="accentRefusal"
        :id="`${name}-accent-refusal`"
        role="alert"
        class="mt-1.5 text-xs text-destructive"
      >
        {{ accentRefusal }}
      </p>
    </div>
  </div>
</template>
