<script setup lang="ts">
import { Search } from "lucide-vue-next";
import { emojiGroups, searchEmoji } from "#layers/ui/app/lib/emoji";
import { Input } from "#layers/ui/app/components/ui/input";

/**
 * Choosing an emoji.
 *
 * Ships with the application rather than pulling an emoji library: every
 * package worth using carries the full Unicode set with names and often sprite
 * images, which is hundreds of kilobytes to several megabytes downloaded by
 * everybody to support a picker most people use for six symbols. The glyphs
 * here come from the reader's own system font.
 */

const emit = defineEmits<{ pick: [emoji: string]; gif: [url: string] }>();

const open = defineModel<boolean>({ default: false });

const query = ref("");
const active = ref(0);
const field = ref<HTMLInputElement>();

/*
 * Which library is being browsed.
 *
 * The GIF tab appears only when this server offers search — an operator has to
 * configure a provider and their own key — so a control that cannot work is
 * never shown.
 */
const tab = ref<"emoji" | "gifs">("emoji");
const gifs = useGifs();

const { $t, $ts } = useI18n();

watch(tab, (chosen) => {
  if (chosen === "gifs") void gifs.run(query.value);
});

const results = computed(() => searchEmoji(query.value));
const searching = computed(() => query.value.trim().length > 0);

watch(open, async (isOpen) => {
  if (!isOpen) {
    query.value = "";
    active.value = 0;
    return;
  }
  await nextTick();
  field.value?.focus();
});

function choose(emoji: string) {
  emit("pick", emoji);
  open.value = false;
}

/** Sends a GIF by its URL, which the composer turns into an attachment. */
function chooseGif(url: string) {
  emit("gif", url);
  open.value = false;
}

// Typing searches whichever library is showing.
watch(query, (text) => {
  if (tab.value === "gifs") gifs.search(text);
});

function onKeydown(event: KeyboardEvent) {
  if (event.key === "Escape") {
    event.preventDefault();
    open.value = false;
    return;
  }

  if (!searching.value || results.value.length === 0) return;

  if (event.key === "Enter") {
    event.preventDefault();
    const chosen = results.value[active.value];
    if (chosen) choose(chosen.value);
    return;
  }

  const step = event.key === "ArrowRight" ? 1 : event.key === "ArrowLeft" ? -1 : 0;
  if (step !== 0) {
    event.preventDefault();
    active.value = (active.value + step + results.value.length) % results.value.length;
  }
}

// Back to the first result whenever the search changes, so the selection is
// never left pointing at a row that has been replaced.
watch(results, () => {
  active.value = 0;
});
</script>

<template>
  <div v-if="open" class="w-72 rounded-xl border border-border bg-card p-2 shadow-xl">
    <!-- Only when the server has a provider configured: a tab for a feature
         that cannot work reads as broken rather than absent. -->
    <div v-if="gifs.available.value" class="mb-2 flex gap-1">
      <button
        v-for="option in ['emoji', 'gifs'] as const"
        :key="option"
        type="button"
        class="flex-1 cursor-pointer rounded-md px-2 py-1 text-xs font-medium transition-colors"
        :class="
          tab === option
            ? 'bg-accent text-accent-foreground'
            : 'text-muted-foreground hover:bg-accent/50'
        "
        @click="tab = option"
      >
        {{ option === "emoji" ? $t("attachment.emoji") : $t("attachment.gifs") }}
      </button>
    </div>

    <div class="relative mb-2">
      <Search
        class="pointer-events-none absolute top-1/2 left-2 size-3.5 -translate-y-1/2 text-muted-foreground"
      />
      <Input
        class="h-8 pl-7 text-xs"
        ref="field"
        v-model="query"
        :placeholder="tab === 'gifs' ? $ts('attachment.searchGifs') : $ts('attachment.searchEmoji')"
        :aria-label="tab === 'gifs' ? $ts('attachment.searchGifs') : $ts('attachment.searchEmoji')"
        autocomplete="off"
        @keydown="onKeydown"
      />
    </div>

    <div class="max-h-64 overflow-y-auto">
      <!-- GIFs, when that tab is showing. -->
      <template v-if="tab === 'gifs'">
        <p v-if="gifs.failed.value" class="px-2 py-6 text-center text-xs text-muted-foreground">
          {{ $t("attachment.gifsUnreachable") }}
        </p>
        <p
          v-else-if="!gifs.results.value.length && !gifs.searching.value"
          class="px-2 py-6 text-center text-xs text-muted-foreground"
        >
          {{ $t("attachment.nothingToShow") }}
        </p>
        <div v-else class="grid grid-cols-2 gap-1">
          <button
            v-for="gif in gifs.results.value"
            :key="gif.id"
            type="button"
            class="cursor-pointer overflow-hidden rounded-md border border-transparent transition hover:border-border-strong"
            :title="gif.description"
            @click="chooseGif(gif.url)"
          >
            <!-- The preview rather than the full animation: a grid of
                 full-size GIFs would be several megabytes. -->
            <img
              :src="gif.previewUrl"
              :alt="gif.description"
              loading="lazy"
              class="h-24 w-full object-cover"
            />
          </button>
        </div>

        <!-- Both providers require naming them. -->
        <p
          v-if="gifs.provider.value"
          class="pt-2 text-center text-[0.625rem] text-muted-foreground"
        >
          {{
            $t("attachment.poweredBy", {
              provider: gifs.provider.value === "tenor" ? "Tenor" : "GIPHY",
            })
          }}
        </p>
      </template>

      <template v-else-if="searching">
        <p v-if="!results.length" class="px-2 py-6 text-center text-xs text-muted-foreground">
          {{ $t("nav.noMatches", { query: query.trim() }) }}
        </p>
        <div v-else class="grid grid-cols-8 gap-0.5">
          <button
            v-for="(item, index) in results"
            :key="item.value"
            type="button"
            class="emoji-button"
            :data-active="active === index"
            :title="item.keywords[0]"
            @click="choose(item.value)"
            @mousemove="active = index"
          >
            {{ item.value }}
          </button>
        </div>
      </template>

      <template v-else>
        <section v-for="group in emojiGroups" :key="group.name" class="mb-2">
          <h3
            class="px-1 pb-1 text-[0.6875rem] font-semibold tracking-wide text-muted-foreground uppercase"
          >
            {{ group.name }}
          </h3>
          <div class="grid grid-cols-8 gap-0.5">
            <button
              v-for="item in group.emoji"
              :key="item.value"
              type="button"
              class="emoji-button"
              :title="item.keywords[0]"
              @click="choose(item.value)"
            >
              {{ item.value }}
            </button>
          </div>
        </section>
      </template>
    </div>
  </div>
</template>

<style scoped>
@reference "#layers/ui/app/assets/css/main.css";

.emoji-button {
  @apply flex h-8 w-8 cursor-pointer items-center justify-center rounded-md text-lg transition-colors hover:bg-accent;
  /* The system's own emoji font, which is what makes this free to ship. */
  font-family:
    "Apple Color Emoji", "Segoe UI Emoji", "Noto Color Emoji", "Twemoji Mozilla", sans-serif;
}

.emoji-button[data-active="true"] {
  @apply bg-accent;
}
</style>
