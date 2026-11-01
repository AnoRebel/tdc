<script setup lang="ts">
import { usePreferredReducedMotion } from "@vueuse/core";
import {
  CircleOff,
  Eye,
  Feather,
  Keyboard,
  Monitor,
  Rows3,
  Rows4,
  Sparkles,
  Type,
  Wind,
} from "lucide-vue-next";

import type { Density, MotionPreference } from "#layers/ui/app/lib/interface";
import { commandKeyLabel } from "#layers/ui/app/lib/platform";
import { Button } from "#layers/ui/app/components/ui/button";
import { Switch } from "#layers/ui/app/components/ui/switch";

/**
 * How the interface behaves: motion, keyboard shortcuts, formatting, density,
 * and which of its parts are shown.
 *
 * A section rather than a page, so it can sit in the web client's settings,
 * in the desktop client's appearance window and in a dialog reachable from
 * the command palette — three places, one set of controls, nothing to drift.
 *
 * Every change applies immediately and is kept in this browser. These are not
 * stored on the account: somebody may well want motion on a desktop and not
 * on a phone, and the server has no preference of this kind to put them in.
 */

defineProps<{
  /** Whether to draw its own heading, which a host with one already omits. */
  heading?: boolean;
}>();

const { $t, $ts } = useI18n();
const { motionPreference, motion, shortcuts, density, formatting, parts, shows, setShown } =
  useInterfacePreferences();
const system = usePreferredReducedMotion();
// Ctrl or ⌘, as this platform writes it: a key name, never translated.
const commandKey = commandKeyLabel();

// Unique per instance: this can be on the settings page and in the dialog at once.
const motionHeading = useId();
const densityHeading = useId();
const partsHeading = useId();
const titleId = useId();

const motionOptions = computed(() => [
  { value: "system" as MotionPreference, icon: Monitor, label: $ts("interface.motion.system") },
  { value: "full" as MotionPreference, icon: Sparkles, label: $ts("interface.motion.full") },
  { value: "reduced" as MotionPreference, icon: Feather, label: $ts("interface.motion.reduced") },
  { value: "off" as MotionPreference, icon: CircleOff, label: $ts("interface.motion.off") },
]);

const densityOptions = computed(() => [
  { value: "comfortable" as Density, icon: Rows3, label: $ts("interface.density.comfortable") },
  { value: "compact" as Density, icon: Rows4, label: $ts("interface.density.compact") },
]);

/** What following the system currently means, said rather than left implied. */
const systemSays = computed(() =>
  system.value === "reduce"
    ? $ts("interface.motion.systemReduces")
    : $ts("interface.motion.systemAllows"),
);
</script>

<template>
  <section class="space-y-5" :aria-labelledby="heading ? titleId : undefined">
    <h2 v-if="heading" :id="titleId" class="flex items-center gap-1.5 text-sm font-semibold">
      <Wind class="size-4" aria-hidden="true" />
      {{ $t("interface.title") }}
    </h2>

    <div>
      <h3 :id="motionHeading" class="text-xs font-semibold">{{ $t("interface.motion.title") }}</h3>
      <p class="mt-1 text-xs text-muted-foreground">{{ $t("interface.motion.hint") }}</p>

      <div class="mt-2 flex flex-wrap gap-1.5" role="group" :aria-labelledby="motionHeading">
        <Button
          v-for="option in motionOptions"
          :key="option.value"
          class="motion-press"
          :variant="motionPreference === option.value ? 'primary' : 'outline'"
          :aria-pressed="motionPreference === option.value"
          @click="motionPreference = option.value"
        >
          <component :is="option.icon" class="size-3.5" aria-hidden="true" />
          {{ option.label }}
        </Button>
      </div>

      <p class="mt-1.5 text-xs text-muted-foreground" aria-live="polite">
        <template v-if="motionPreference === 'system'">{{ systemSays }}</template>
        {{ $t(`interface.motion.describes.${motion}`) }}
      </p>
    </div>

    <div>
      <h3 class="flex items-center gap-1.5 text-xs font-semibold">
        <Keyboard class="size-3.5" aria-hidden="true" />
        {{ $t("interface.shortcutsLabel") }}
      </h3>
      <div class="mt-2 flex items-start gap-2.5">
        <Switch v-model="shortcuts" :label="$ts('interface.shortcutsLabel')" class="mt-0.5" />
        <p class="text-xs text-muted-foreground">
          {{ $t("interface.shortcutsHint", { key: `${commandKey} K` }) }}
        </p>
      </div>
    </div>

    <div>
      <h3 class="flex items-center gap-1.5 text-xs font-semibold">
        <Type class="size-3.5" aria-hidden="true" />
        {{ $t("interface.formatting.title") }}
      </h3>
      <div class="mt-2 flex items-start gap-2.5">
        <Switch v-model="formatting" :label="$ts('interface.formatting.label')" class="mt-0.5" />
        <div>
          <p class="text-xs">{{ $t("interface.formatting.label") }}</p>
          <p class="text-xs text-muted-foreground">{{ $t("interface.formatting.hint") }}</p>
        </div>
      </div>
    </div>

    <div>
      <h3 :id="densityHeading" class="text-xs font-semibold">
        {{ $t("interface.density.title") }}
      </h3>
      <div class="mt-2 flex flex-wrap gap-1.5" role="group" :aria-labelledby="densityHeading">
        <Button
          v-for="option in densityOptions"
          :key="option.value"
          class="motion-press"
          :variant="density === option.value ? 'primary' : 'outline'"
          :aria-pressed="density === option.value"
          @click="density = option.value"
        >
          <component :is="option.icon" class="size-3.5" aria-hidden="true" />
          {{ option.label }}
        </Button>
      </div>
    </div>

    <div>
      <h3 :id="partsHeading" class="flex items-center gap-1.5 text-xs font-semibold">
        <Eye class="size-3.5" aria-hidden="true" />
        {{ $t("interface.parts.title") }}
      </h3>
      <p class="mt-1 text-xs text-muted-foreground">{{ $t("interface.parts.hint") }}</p>
      <ul class="mt-2 space-y-2" :aria-labelledby="partsHeading">
        <li v-for="part in parts" :key="part" class="flex items-center gap-2.5">
          <Switch
            :model-value="shows(part)"
            :label="$ts(`interface.parts.${part}`)"
            @update:model-value="setShown(part, $event)"
          />
          <span class="text-xs">{{ $t(`interface.parts.${part}`) }}</span>
        </li>
      </ul>
    </div>
  </section>
</template>
