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
  /**
   * A choice rather than an action: "radio" for one of several, "check" for
   * on or off. Announced as such, with whether it is the current one.
   */
  choice?: "radio" | "check";
  checked?: boolean;
  /** Draws a line above this item, between groups of related actions. */
  separated?: boolean;
  /**
   * Starts a titled group with this item. The title is for the eye; each item
   * in the group says in its own label what it does, because a screen reader
   * moving through items does not read headings between them.
   */
  section?: string;
  action: () => void;
}

const props = defineProps<{
  items: MenuItem[];
  /** Where the menu was asked for, in viewport coordinates. */
  x: number;
  y: number;
  /** What the menu is about, for a screen reader announcing it. */
  label?: string;
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

/*
 * Focus goes back to whatever opened the menu when it closes, so a keyboard
 * user who opened it from a button is left on that button rather than at the
 * top of the page.
 */
const opener = import.meta.client ? (document.activeElement as HTMLElement | null) : null;
onBeforeUnmount(() => opener?.focus?.());

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
        :aria-label="props.label"
        :aria-activedescendant="`menu-item-${focused}`"
        class="animate-in fade-in zoom-in-95 fixed max-h-[calc(100dvh-1rem)] min-w-44 overflow-y-auto rounded-lg border bg-surface py-1 shadow-lg duration-100 outline-none"
        :style="position"
        @click.stop
        @keydown.down.prevent="move(1)"
        @keydown.up.prevent="move(-1)"
        @keydown.home.prevent="focused = 0"
        @keydown.end.prevent="focused = enabled.length - 1"
        @keydown.enter.prevent="activateFocused"
        @keydown.space.prevent="activateFocused"
        @keydown.tab.prevent="emit('close')"
      >
        <template v-for="(item, index) in enabled" :key="item.label">
          <div
            v-if="(item.separated || item.section) && index > 0"
            role="separator"
            class="my-1 border-t"
          />
          <div
            v-if="item.section"
            aria-hidden="true"
            class="px-3 pt-1.5 pb-1 text-[0.6875rem] font-semibold tracking-wide text-muted-foreground uppercase"
          >
            {{ item.section }}
          </div>
          <button
            :id="`menu-item-${index}`"
            type="button"
            :role="
              item.choice === 'radio'
                ? 'menuitemradio'
                : item.choice === 'check'
                  ? 'menuitemcheckbox'
                  : 'menuitem'
            "
            :aria-checked="item.choice ? Boolean(item.checked) : undefined"
            tabindex="-1"
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
            <span class="min-w-0 flex-1 truncate">{{ item.label }}</span>
            <!-- The current choice, marked by shape as well as by the
                 announcement, so it is not told by colour alone. -->
            <Icon
              v-if="item.choice && item.checked"
              name="lucide:check"
              class="size-3.5 shrink-0 text-primary"
              aria-hidden="true"
            />
          </button>
        </template>
      </div>
    </div>
  </Teleport>
</template>
