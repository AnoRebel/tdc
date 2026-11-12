<script setup lang="ts">
import { Check, Monitor, Moon, Palette, Sun } from "lucide-vue-next";
import { Button } from "#layers/ui/app/components/ui/button";
import { isMaterial } from "#layers/ui/app/lib/theme";

/**
 * Changing how the interface looks, without going to settings.
 *
 * The same three choices settings offers — the palette, light or dark, and
 * whether to use a custom accent — because somebody trying colours wants to
 * see the result immediately rather than after a navigation. Settings keeps
 * the accent field, which needs a keyboard and a refusal message; this is for
 * the choices that are one press each.
 *
 * Both clients get it: the desktop application has no settings URL to send
 * somebody to, so this is the only way to change the theme there.
 */

const open = defineModel<boolean>({ default: false });

const { $t, $ts } = useI18n();

const theme = useTheme();
const showingAppearance = useState("appearance.open", () => false);

/** The rest — materials, contrast, glass, accent — in the appearance dialog. */
function more() {
  open.value = false;
  showingAppearance.value = true;
}

const appearances = computed(() => [
  { value: "auto" as const, label: $t("settings.followMyDevice"), icon: Monitor },
  { value: "light" as const, label: $t("settings.light"), icon: Sun },
  { value: "dark" as const, label: $t("settings.dark"), icon: Moon },
]);

const palettes = computed(() => [
  { value: "default" as const, label: $t("app.paletteCool"), swatch: "oklch(0.53 0.16 269)" },
  { value: "congrator" as const, label: $t("app.paletteWarm"), swatch: "oklch(0.62 0.16 40)" },
]);

const panel = useTemplateRef<HTMLElement>("panel");

/*
 * Closing it the two ways somebody expects.
 *
 * Escape and a press outside. Without both, a popover opened by accident can
 * only be dismissed by choosing something from it — which means changing a
 * setting to get rid of a menu.
 */
onClickOutside(panel, () => {
  open.value = false;
});

function onKeydown(event: KeyboardEvent) {
  if (event.key === "Escape") open.value = false;
}
</script>

<template>
  <div class="relative" @keydown="onKeydown">
    <Button
      variant="ghost"
      size="icon"
      :label="$ts('settings.appearance')"
      :aria-expanded="open"
      aria-haspopup="menu"
      @click="open = !open"
    >
      <Palette class="size-3.5" aria-hidden="true" />
    </Button>

    <div
      v-if="open"
      ref="panel"
      role="menu"
      :aria-label="$ts('settings.appearance')"
      class="motion-pop-in absolute right-0 bottom-full z-30 origin-bottom-right mb-2 w-52 overflow-hidden rounded-xl border border-border bg-surface-raised p-1 shadow-lg"
    >
      <p class="px-2 pt-1.5 pb-1 text-[0.6875rem] font-semibold text-muted-foreground uppercase">
        {{ $t("settings.appearance") }}
      </p>
      <button
        v-for="option in appearances"
        :key="option.value"
        type="button"
        role="menuitemradio"
        :aria-checked="theme.preference.value === option.value"
        class="flex w-full items-center gap-2 rounded-lg px-2 py-1.5 text-xs hover:bg-muted"
        @click="theme.set(option.value)"
      >
        <component :is="option.icon" class="size-3.5 shrink-0 opacity-70" />
        <span class="flex-1 text-left">{{ option.label }}</span>
        <!-- A tick rather than a highlight: which one is active should survive
             somebody's cursor being somewhere else. -->
        <Check v-if="theme.preference.value === option.value" class="size-3.5 shrink-0" />
      </button>

      <div class="my-1 border-t border-border" />

      <p class="px-2 pt-1 pb-1 text-[0.6875rem] font-semibold text-muted-foreground uppercase">
        {{ $t("app.colours") }}
      </p>
      <button
        v-for="option in palettes"
        :key="option.value"
        type="button"
        role="menuitemradio"
        :aria-checked="theme.palette.value === option.value"
        class="flex w-full items-center gap-2 rounded-lg px-2 py-1.5 text-xs hover:bg-muted"
        @click="theme.setPalette(option.value)"
      >
        <!-- The palette's own primary, so the choice is shown rather than
             described: "Warm" means little until you have seen it. -->
        <span
          class="size-3.5 shrink-0 rounded-full border border-border"
          :style="{ background: option.swatch }"
        />
        <span class="flex-1 text-left">{{ option.label }}</span>
        <Check v-if="theme.palette.value === option.value" class="size-3.5 shrink-0" />
      </button>
      <!-- Ticked while a material is the colours in use, so the menu never
           shows neither palette chosen without saying why. -->
      <button
        type="button"
        role="menuitem"
        class="flex w-full items-center gap-2 rounded-lg px-2 py-1.5 text-xs hover:bg-muted"
        @click="more"
      >
        <Palette class="size-3.5 shrink-0 opacity-70" />
        <span class="flex-1 text-left">{{ $t("appearance.more") }}</span>
        <Check v-if="isMaterial(theme.palette.value)" class="size-3.5 shrink-0" />
      </button>

      <!-- Only when there is one to clear. A reset for something nobody set is
           a control that does nothing, which teaches people to ignore the
           menu. -->
      <template v-if="theme.accent.value">
        <div class="my-1 border-t border-border" />
        <button
          type="button"
          role="menuitem"
          class="flex w-full items-center gap-2 rounded-lg px-2 py-1.5 text-xs hover:bg-muted"
          @click="theme.setAccent('')"
        >
          <span
            class="size-3.5 shrink-0 rounded-full border border-border"
            :style="{ background: theme.accent.value }"
          />
          <span class="flex-1 text-left">{{ $t("app.resetAccent") }}</span>
        </button>
      </template>
    </div>
  </div>
</template>
