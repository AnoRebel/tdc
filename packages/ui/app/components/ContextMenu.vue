<script setup lang="ts">
import type { Component } from "vue";

/**
 * A menu of actions, opened where the person asked for it.
 *
 * Right-click with a pointer, long press with a finger. A touch screen has no
 * right button and no hover, so an interface whose actions only appear on hover
 * is unusable on a phone — which is how most TDC members will reach this.
 *
 * The menu is always a shortcut. Every action it lists is reachable another way,
 * so nobody is stranded because they did not know to press and hold.
 */

export interface MenuItem {
  label: string;
  icon?: Component;
  /** Marks an action that destroys something, so it is styled as a warning. */
  destructive?: boolean;
  /** Absent actions are not rendered; a menu never lists what cannot be done. */
  disabled?: boolean;
  action: () => void;
}

const props = defineProps<{
  items: MenuItem[];
  /** Where the menu was asked for, in viewport coordinates. */
  x: number;
  y: number;
}>();

const emit = defineEmits<{ close: [] }>();

const menu = ref<HTMLElement | null>(null);
const focused = ref(0);

/**
 * Keeps the menu on screen.
 *
 * Opened near the right or bottom edge, a menu placed exactly where the pointer
 * was would hang off the page — most likely on a phone, where the edges are
 * close to everything.
 */
const position = ref({ left: `${props.x}px`, top: `${props.y}px` });

onMounted(async () => {
  await nextTick();

  const element = menu.value;
  if (!element) return;

  /*
   * offsetWidth/offsetHeight rather than getBoundingClientRect.
   *
   * The menu opens with a zoom-in animation, and a bounding rect reports the
   * box as it is currently drawn — mid-animation, that is 95% of the real
   * size. Clamping against a height that is too small lets a menu opened near
   * the bottom of the window overhang it once the animation finishes, which is
   * exactly the case this function exists for. Offset dimensions ignore
   * transforms and give the settled size straight away.
   */
  const width = element.offsetWidth;
  const height = element.offsetHeight;
  const margin = 8;

  const left = Math.min(props.x, window.innerWidth - width - margin);
  const top = Math.min(props.y, window.innerHeight - height - margin);

  position.value = {
    left: `${Math.max(margin, left)}px`,
    top: `${Math.max(margin, top)}px`,
  };

  element.focus();
});

const enabled = computed(() => props.items.filter((item) => !item.disabled));

function choose(item: MenuItem) {
  emit("close");
  item.action();
}

function move(delta: number) {
  const count = enabled.value.length;
  if (count === 0) return;
  focused.value = (focused.value + delta + count) % count;
}

function activateFocused() {
  const item = enabled.value[focused.value];
  if (item) choose(item);
}

// Escape closes, as it must: a menu that traps someone is worse than no menu.
onKeyStroke("Escape", () => emit("close"));

// The page must not scroll behind an open menu, or the menu ends up pointing
// at whatever moved under it.
onMounted(() => {
  document.body.style.overflow = "hidden";
});
onBeforeUnmount(() => {
  document.body.style.overflow = "";
});
</script>

<template>
  <Teleport to="body">
    <!-- A full-screen backdrop rather than a document listener: it catches the
         click that dismisses the menu, and on a touch screen it stops the press
         reaching whatever is underneath. -->
    <div class="fixed inset-0 z-50" @click="emit('close')" @contextmenu.prevent="emit('close')">
      <div
        ref="menu"
        role="menu"
        tabindex="-1"
        class="animate-in fade-in zoom-in-95 fixed min-w-44 overflow-hidden rounded-lg border bg-surface py-1 shadow-lg duration-100 outline-none"
        :style="position"
        @click.stop
        @keydown.down.prevent="move(1)"
        @keydown.up.prevent="move(-1)"
        @keydown.enter.prevent="activateFocused"
      >
        <button
          v-for="(item, index) in enabled"
          :key="item.label"
          type="button"
          role="menuitem"
          class="flex w-full items-center gap-2.5 px-3 py-2 text-left text-sm transition-colors"
          :class="[
            item.destructive ? 'text-destructive' : 'text-foreground',
            index === focused
              ? item.destructive
                ? 'bg-destructive/10'
                : 'bg-accent'
              : 'hover:bg-accent',
          ]"
          @click="choose(item)"
          @mouseenter="focused = index"
        >
          <component :is="item.icon" v-if="item.icon" class="size-3.5 shrink-0 opacity-70" />
          {{ item.label }}
        </button>
      </div>
    </div>
  </Teleport>
</template>
