<script setup lang="ts">
import { nameOf } from "#layers/ui/app/lib/names";
import { Hash, Search } from "lucide-vue-next";

import { Avatar } from "#layers/ui/app/components/ui/avatar";
import { Spinner } from "#layers/ui/app/components/ui/spinner";

/**
 * Finding somebody, or a channel, by typing.
 *
 * A combobox: the box keeps focus while the arrow keys move through what
 * matched, Enter takes the one highlighted, Escape closes the list. The list
 * is announced as it changes, so somebody not seeing it knows whether anything
 * matched.
 */

const { $t, $ts } = useI18n();

const props = defineProps<{
  kind: "people" | "rooms";
  label: string;
  placeholder?: string;
  /** Identifiers not to offer — people already in the role, say. */
  exclude?: string[];
}>();

const emit = defineEmits<{ pick: [choice: { id: string; name: string; detail?: string }] }>();

const roles = useRoles();
const listId = useId();
const text = ref("");
const typed = refDebounced(text, 200);
const results = ref<{ id: string; name: string; detail?: string }[]>([]);
const searching = ref(false);
const open = ref(false);
const active = ref(0);

watch(typed, async (value) => {
  if (!value.trim()) {
    results.value = [];
    return;
  }
  searching.value = true;
  try {
    const found = await roles.search(value);
    const excluded = new Set(props.exclude ?? []);
    results.value =
      props.kind === "people"
        ? found.people
            .filter((person) => !excluded.has(person.id))
            .map((person) => ({
              id: person.id,
              name: nameOf(person),
              detail: person.address,
            }))
        : found.rooms
            .filter((room) => !excluded.has(room.id))
            .map((room) => ({ id: room.id, name: room.name }));
    active.value = 0;
    open.value = true;
  } catch {
    results.value = [];
  } finally {
    searching.value = false;
  }
});

function choose(index: number) {
  const choice = results.value[index];
  if (!choice) return;
  emit("pick", choice);
  text.value = "";
  results.value = [];
  open.value = false;
}

function move(by: number) {
  if (!results.value.length) return;
  open.value = true;
  active.value = (active.value + by + results.value.length) % results.value.length;
}

const optionId = (index: number) => `${listId}-${index}`;
</script>

<template>
  <div class="relative">
    <div class="relative">
      <Search
        class="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-muted-foreground"
        aria-hidden="true"
      />
      <input
        v-model="text"
        type="search"
        role="combobox"
        autocomplete="off"
        class="h-8 w-full rounded-md border border-border bg-surface pr-8 pl-8 text-sm placeholder:text-muted-foreground focus-visible:border-ring focus-visible:ring-2 focus-visible:ring-ring/40 focus-visible:outline-none"
        :aria-label="props.label"
        :placeholder="props.placeholder"
        :aria-expanded="open && results.length > 0"
        :aria-controls="listId"
        aria-autocomplete="list"
        :aria-activedescendant="open && results.length ? optionId(active) : undefined"
        @keydown.down.prevent="move(1)"
        @keydown.up.prevent="move(-1)"
        @keydown.enter.prevent="choose(active)"
        @keydown.esc="open = false"
        @blur="open = false"
      />
      <Spinner v-if="searching" size="sm" class="absolute top-1/2 right-2.5 -translate-y-1/2" />
    </div>

    <ul
      v-show="open && results.length"
      :id="listId"
      role="listbox"
      :aria-label="props.label"
      class="animate-in fade-in absolute z-20 mt-1 max-h-64 w-full overflow-y-auto rounded-md border border-border bg-card p-1 shadow-lg duration-(--motion-duration-fast)"
    >
      <!-- Chosen with the pointer on mousedown, before the box loses focus and
           closes the list; the keyboard path is the box's own Enter. -->
      <!-- eslint-disable-next-line vuejs-accessibility/click-events-have-key-events -->
      <li
        v-for="(result, index) in results"
        :id="optionId(index)"
        :key="result.id"
        role="option"
        :aria-selected="index === active"
        class="flex cursor-pointer items-center gap-2 rounded px-2 py-1.5 text-sm"
        :class="index === active ? 'bg-accent text-accent-foreground' : ''"
        @mousedown.prevent="choose(index)"
        @mouseenter="active = index"
      >
        <Avatar v-if="props.kind === 'people'" :name="result.name" size="sm" />
        <Hash v-else class="size-3.5 text-muted-foreground" aria-hidden="true" />
        <span class="min-w-0 flex-1 truncate">{{ result.name }}</span>
        <span v-if="result.detail" class="truncate text-xs text-muted-foreground">
          {{ result.detail }}
        </span>
      </li>
    </ul>

    <p class="sr-only" role="status">
      <template v-if="typed.trim() && !searching">
        {{
          results.length
            ? $ts("roles.picker.found", { count: results.length })
            : $t("roles.picker.nothing")
        }}
      </template>
    </p>
  </div>
</template>
