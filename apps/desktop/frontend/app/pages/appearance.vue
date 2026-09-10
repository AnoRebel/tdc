<script setup lang="ts">
/**
 * How the application looks.
 *
 * The palette and light-or-dark come from the shared toggle, which both
 * clients have. What is here instead of there is the one thing a browser tab
 * cannot do: show what is behind it.
 */
useHead({ title: "Appearance" });

const { opacity, refresh, set } = useWindowOpacity();

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

/*
 * The palette and light-or-dark, as buttons rather than the sidebar's toggle.
 *
 * That toggle is built for a footer: it opens upwards and sizes itself to a
 * narrow strip, which on a full page leaves stretched labels and collapsed
 * swatches. A settings screen has room to show the choices outright, and a
 * menu that has to be opened to see what is selected is worse here anyway.
 */
const theme = useTheme();

const appearances = [
  { value: "auto" as const, label: "Follow my device" },
  { value: "light" as const, label: "Light" },
  { value: "dark" as const, label: "Dark" },
];

const palettes = [
  { value: "default" as const, label: "Cool", description: "Indigo on tinted grey" },
  { value: "congrator" as const, label: "Warm", description: "Terracotta and amber" },
];

onMounted(async () => {
  await refresh();
  running.value = opacity.value;
});
</script>

<template>
  <main class="mx-auto max-w-lg p-8">
    <h1 class="text-lg font-semibold">Appearance</h1>

    <section class="mt-5">
      <h2 class="text-sm font-semibold">Colours</h2>
      <p class="mt-1 text-xs text-muted-foreground">
        The palette, and whether to follow your device's light or dark setting.
      </p>

      <h3 class="mt-4 text-xs font-semibold">Light or dark</h3>
      <div class="mt-2 flex flex-wrap gap-1.5">
        <button
          v-for="option in appearances"
          :key="option.value"
          type="button"
          class="btn"
          :class="theme.preference.value === option.value ? 'btn-primary' : 'btn-outline'"
          :aria-pressed="theme.preference.value === option.value"
          @click="theme.set(option.value)"
        >
          {{ option.label }}
        </button>
      </div>

      <h3 class="mt-4 text-xs font-semibold">Palette</h3>
      <div class="mt-2 flex flex-wrap gap-1.5">
        <button
          v-for="option in palettes"
          :key="option.value"
          type="button"
          class="btn"
          :class="theme.palette.value === option.value ? 'btn-primary' : 'btn-outline'"
          :aria-pressed="theme.palette.value === option.value"
          @click="theme.setPalette(option.value)"
        >
          {{ option.label }}
          <span class="text-[0.6875rem] opacity-70">{{ option.description }}</span>
        </button>
      </div>
    </section>

    <section class="mt-5">
      <h2 class="text-sm font-semibold">Window</h2>
      <p class="mt-1 text-xs text-muted-foreground">
        How much of what is behind TDC shows through. Only the empty space becomes see-through —
        messages and menus stay on something solid.
      </p>

      <div class="mt-3 flex items-center gap-3">
        <label class="sr-only" for="opacity">Window opacity</label>
        <input
          id="opacity"
          type="range"
          min="50"
          max="100"
          step="1"
          class="flex-1"
          :value="opacity"
          :aria-valuetext="`${opacity} per cent solid`"
          @input="set(Number(($event.target as HTMLInputElement).value))"
        />
        <span class="tabular w-12 shrink-0 text-right text-xs">{{ opacity }}%</span>
      </div>

      <p v-if="ignoredHere" class="mt-2 text-xs text-warning">
        Your desktop ignores this at the moment. The toolkit TDC is built on has not implemented
        window transparency on Linux yet, so the window stays solid whatever this says. The setting
        is kept because it will work as soon as that lands.
      </p>
      <p v-else-if="pending" class="mt-2 text-xs text-warning">
        Reopen TDC to see this. A window's transparency is fixed when it opens, so this one stays at
        {{ running }}% until then.
      </p>
      <p class="mt-2 text-xs text-muted-foreground">
        The slider stops at half, because below that text on any background becomes a struggle.
      </p>
    </section>
  </main>
</template>
