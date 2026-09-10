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
 * The palette and light-or-dark, from the same toggle the sidebar carries —
 * so somebody looking for "how this looks" finds all of it in one place
 * rather than two.
 */
const showTheme = ref(false);

onMounted(() => void refresh());
</script>

<template>
  <main class="mx-auto max-w-lg p-8">
    <h1 class="text-lg font-semibold">Appearance</h1>

    <section class="mt-5">
      <h2 class="text-sm font-semibold">Colours</h2>
      <p class="mt-1 text-xs text-muted-foreground">
        The palette, and whether to follow your device's light or dark setting.
      </p>

      <div class="mt-3">
        <ThemeToggle v-model="showTheme" />
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

      <p class="mt-2 text-xs text-muted-foreground">
        The slider stops at half, because below that text on any background becomes a struggle.
      </p>
    </section>
  </main>
</template>
