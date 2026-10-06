<script setup lang="ts">
import { useLocalStorage } from "@vueuse/core";

/**
 * A titled group in the sidebar, which somebody can fold away.
 *
 * Somebody in forty channels who only reads their direct messages should not
 * scroll past forty channels to reach them. What is folded is remembered in
 * this browser, per section, and stays folded across visits.
 *
 * The heading is a real button with aria-expanded, so the state is announced
 * and the section can be opened from the keyboard. The contents fade and
 * settle rather than the section animating its height, which would lay the
 * whole sidebar out again on every frame.
 */
const props = defineProps<{
  /** Names the section in storage; stable across languages. */
  id: string;
  title: string;
}>();

const folded = useLocalStorage<string[]>("tdc-sidebar-folded", [], { writeDefaults: false });
const open = computed(() => !folded.value.includes(props.id));

function toggle() {
  folded.value = open.value
    ? [...folded.value, props.id]
    : folded.value.filter((id) => id !== props.id);
}

const contentId = `sidebar-section-${props.id}`;
</script>

<template>
  <section>
    <h2>
      <button
        type="button"
        class="motion-row flex w-full items-center gap-1 rounded-sm px-2 pb-1 text-[0.6875rem] font-semibold tracking-wide text-muted-foreground uppercase hover:text-foreground"
        :aria-expanded="open"
        :aria-controls="contentId"
        @click="toggle"
      >
        <Icon
          name="lucide:chevron-right"
          class="size-3 shrink-0 transition-transform duration-(--motion-duration-fast) ease-(--motion-ease-out)"
          :class="open && 'rotate-90'"
          aria-hidden="true"
        />
        <span class="truncate">{{ title }}</span>
      </button>
    </h2>
    <Transition name="motion-collapse">
      <div v-show="open" :id="contentId">
        <slot />
      </div>
    </Transition>
  </section>
</template>
