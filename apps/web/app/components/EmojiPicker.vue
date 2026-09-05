<script setup lang="ts">
import { Search } from "lucide-vue-next";
import { emojiGroups, searchEmoji } from "~/lib/emoji";

/**
 * Choosing an emoji.
 *
 * Ships with the application rather than pulling an emoji library: every
 * package worth using carries the full Unicode set with names and often sprite
 * images, which is hundreds of kilobytes to several megabytes downloaded by
 * everybody to support a picker most people use for six symbols. The glyphs
 * here come from the reader's own system font.
 */

const emit = defineEmits<{ pick: [emoji: string] }>();

const open = defineModel<boolean>({ default: false });

const query = ref("");
const active = ref(0);
const field = ref<HTMLInputElement>();

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
    <div class="relative mb-2">
      <Search
        class="pointer-events-none absolute top-1/2 left-2 size-3.5 -translate-y-1/2 text-muted-foreground"
      />
      <input
        ref="field"
        v-model="query"
        type="text"
        placeholder="Search emoji"
        class="input h-8 pl-7 text-xs"
        aria-label="Search emoji"
        autocomplete="off"
        @keydown="onKeydown"
      />
    </div>

    <div class="max-h-64 overflow-y-auto">
      <template v-if="searching">
        <p v-if="!results.length" class="px-2 py-6 text-center text-xs text-muted-foreground">
          Nothing matches &ldquo;{{ query.trim() }}&rdquo;.
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
@reference "~/assets/css/main.css";

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
