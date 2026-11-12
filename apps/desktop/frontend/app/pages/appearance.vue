<script setup lang="ts">
import { Checkbox } from "#layers/ui/app/components/ui/checkbox";
import { ArrowLeft } from "lucide-vue-next";

const { $t, $ts } = useI18n();

const router = useRouter();

/**
 * How the application looks.
 *
 * The colours and the interface preferences are the shared ones, the same
 * as the web client's settings. What is here besides is the one thing a
 * browser tab cannot do: show what is behind it.
 */
/*
 * In the application's own frame, like every other screen somebody reaches
 * while signed in: a call that rings while this is open is answered here, not
 * only from the system's banner.
 */
definePageMeta({ middleware: "auth", layout: "app" });

useHead({ title: $ts("settings.appearance") });

const { opacity, refresh, set } = useWindowOpacity();

/*
 * Opening at login, and opening to the tray. On this screen because it is
 * where the desktop client's own settings live.
 */
const startup = useStartup();

/*
 * What the window is actually running at, kept apart from what the slider
 * shows. They differ the moment somebody moves it, and saying so is the whole
 * point — a slider that appears to do nothing reads as broken.
 */
const running = ref(100);
const pending = computed(() => running.value !== opacity.value);

/*
 * Whether this platform does anything with it.
 *
 * Linux does not today: Wails leaves its GTK4 transparency function empty, so
 * the window is never given a background to composite against. Saying so is
 * the only honest thing — a slider that quietly does nothing is a bug report
 * waiting to be written.
 */
const ignoredHere = ref(false);
onMounted(() => {
  ignoredHere.value = navigator.userAgent.includes("Linux");
});

onMounted(async () => {
  await refresh();
  running.value = opacity.value;
  await startup.refresh();
});
</script>

<template>
  <main class="mx-auto max-w-lg p-8">
    <!-- Back to wherever this was opened from: the menu or settings. -->
    <button type="button" class="btn btn-ghost mb-4 -ml-2 h-7 px-2 text-xs" @click="router.back()">
      <ArrowLeft class="size-3.5" />
      {{ $t("action.back") }}
    </button>
    <h1 class="text-lg font-semibold">{{ $t("settings.appearance") }}</h1>

    <!-- The same chooser as the web client's settings and the appearance
         dialog: light or dark, contrast, colours and Crystal Neuo's materials,
         glass and accent. Shown outright rather than as the sidebar's menu,
         which is built for a narrow footer. -->
    <section class="mt-5">
      <AppearanceChooser />
    </section>

    <section class="mt-5">
      <InterfacePreferences heading />
    </section>

    <section class="mt-5">
      <h2 class="text-sm font-semibold">{{ $t("desktop.appearance.window") }}</h2>
      <p class="mt-1 text-xs text-muted-foreground">
        {{ $t("desktop.appearance.windowHint") }}
      </p>

      <div class="mt-3 flex items-center gap-3">
        <label class="sr-only" for="opacity">{{ $t("desktop.appearance.opacity") }}</label>
        <input
          id="opacity"
          type="range"
          min="50"
          max="100"
          step="1"
          class="flex-1"
          :value="opacity"
          :aria-valuetext="$ts('desktop.appearance.perCentSolid', { value: opacity })"
          @input="set(Number(($event.target as HTMLInputElement).value))"
        />
        <span class="tabular w-12 shrink-0 text-right text-xs">{{ opacity }}%</span>
      </div>

      <p v-if="ignoredHere" class="mt-2 text-xs text-warning">
        {{ $t("desktop.appearance.ignoredOnLinux") }}
      </p>
      <p v-else-if="pending" class="mt-2 text-xs text-warning">
        {{ $t("desktop.appearance.reopenToSee", { percent: running }) }}
      </p>
      <p class="mt-2 text-xs text-muted-foreground">
        {{ $t("desktop.appearance.sliderStops") }}
      </p>
    </section>
    <section class="mt-8">
      <h2 class="text-sm font-semibold">{{ $t("desktop.appearance.starting") }}</h2>
      <p class="mt-1 text-xs text-muted-foreground">
        {{ $t("desktop.appearance.startingHint") }}
      </p>

      <!-- The label wraps its control, which is how HTML associates the
           two. The rule cannot see the input inside <Checkbox>. -->
      <!-- eslint-disable-next-line vuejs-accessibility/label-has-for -->
      <label class="mt-3 flex items-start gap-2 text-xs">
        <Checkbox
          class="mt-0.5"
          :model-value="startup.atLogin.value"
          @update:model-value="startup.setAtLogin($event)"
        />
        <span>
          {{ $t("desktop.appearance.openAtLogin") }}
          <span class="block text-muted-foreground">{{
            $t("desktop.appearance.openAtLoginHint")
          }}</span>
        </span>
      </label>

      <!-- The label wraps its control, which is how HTML associates the
           two. The rule cannot see the input inside <Checkbox>. -->
      <!-- eslint-disable-next-line vuejs-accessibility/label-has-for -->
      <label class="mt-2 flex items-start gap-2 text-xs">
        <Checkbox
          class="mt-0.5"
          :model-value="startup.hidden.value"
          @update:model-value="startup.setHidden($event)"
        />
        <span>
          {{ $t("desktop.appearance.startInTray") }}
          <span class="block text-muted-foreground">
            {{ $t("desktop.appearance.startInTrayHint") }}
          </span>
        </span>
      </label>

      <!-- Some desktops have nowhere to register this. Saying so beats a
           switch that quietly returns to off. -->
      <p v-if="startup.problem.value" role="alert" class="mt-2 text-xs text-destructive">
        {{ startup.problem.value }}
      </p>
    </section>
  </main>
</template>
