<script setup lang="ts">
import type { Room } from "@tdc/types";
import { Avatar } from "#layers/ui/app/components/ui/avatar";
import { otherParticipants } from "#layers/ui/app/lib/sidebar";

/**
 * One conversation in the sidebar.
 *
 * Its menu opens three ways: right-click or long press on the row, the
 * keyboard's own menu key while the row is focused, and the "more" button that
 * appears on hover and on focus — the one a keyboard user can Tab to and a
 * newcomer can see is there.
 */

const props = defineProps<{
  room: Room;
  unread?: number;
  /** Whether an unread count is worth a badge here; not when muted. */
  badge: boolean;
  muted: boolean;
  current: boolean;
  /** A channel the person has not joined, drawn quieter than the rest. */
  joinable?: boolean;
}>();

const emit = defineEmits<{
  /** The menu was asked for: at a point, or below an element. */
  menu: [at: { x: number; y: number } | Element];
}>();

const { $ts } = useI18n();
const { user } = useAuth();

const others = computed(() =>
  props.room.kind === "direct" ? otherParticipants(props.room, user.value?.displayName) : [],
);
const name = computed(() =>
  props.room.kind === "direct" ? others.value.join(", ") : props.room.name,
);
const group = computed(() => others.value.length > 1);

let pressTimer: ReturnType<typeof setTimeout> | undefined;
let pressedAt = { x: 0, y: 0 };
let longPressed = false;

function onContextmenu(event: MouseEvent) {
  event.preventDefault();
  // The menu key and Shift+F10 arrive with no pointer position.
  if (!event.clientX && !event.clientY) emit("menu", event.currentTarget as Element);
  else emit("menu", { x: event.clientX, y: event.clientY });
}

/*
 * A long press, for a finger: there is no right button on a phone. Moving
 * more than a few pixels is a scroll, not a press.
 */
function onTouchstart(event: TouchEvent) {
  const touch = event.touches[0];
  if (!touch) return;
  pressedAt = { x: touch.clientX, y: touch.clientY };
  longPressed = false;
  pressTimer = setTimeout(() => {
    longPressed = true;
    navigator.vibrate?.(10);
    emit("menu", pressedAt);
  }, 500);
}

function onTouchmove(event: TouchEvent) {
  const touch = event.touches[0];
  if (touch && Math.abs(touch.clientX - pressedAt.x) + Math.abs(touch.clientY - pressedAt.y) > 10) {
    clearTimeout(pressTimer);
  }
}

function onClick(event: MouseEvent) {
  // The release that ends a long press is not also a click on the row.
  if (longPressed) {
    event.preventDefault();
    longPressed = false;
  }
}

function cancelPress() {
  clearTimeout(pressTimer);
}

onBeforeUnmount(cancelPress);
</script>

<template>
  <li class="group/row relative">
    <NuxtLink
      :to="`/rooms/${room.id}`"
      class="nav-item pr-7"
      :data-active="current"
      :aria-current="current ? 'page' : undefined"
      @contextmenu="onContextmenu"
      @touchstart.passive="onTouchstart"
      @touchmove.passive="onTouchmove"
      @touchend="cancelPress"
      @touchcancel="cancelPress"
      @click="onClick"
    >
      <!-- Two faces for a group, overlapped, so a conversation with several
           people reads differently from one with somebody alone. -->
      <span
        v-if="room.kind === 'direct' && group"
        class="relative size-5 shrink-0"
        aria-hidden="true"
      >
        <Avatar
          :name="others[0]!"
          size="sm"
          class="absolute top-0 left-0 size-3.5 text-[0.4375rem]"
        />
        <Avatar
          :name="others[1]!"
          size="sm"
          class="absolute right-0 bottom-0 size-3.5 text-[0.4375rem] ring-2 ring-surface-sunken"
        />
      </span>
      <Avatar
        v-else-if="room.kind === 'direct'"
        :name="name"
        size="sm"
        class="size-5 shrink-0 text-[0.5rem]"
      />
      <Icon
        v-else
        :name="room.visibility === 'private' ? 'lucide:lock' : 'lucide:hash'"
        class="size-3.5 shrink-0"
        :class="joinable ? 'opacity-40' : 'opacity-60'"
      />

      <span class="truncate" :class="(muted || joinable) && 'opacity-60'">{{ name }}</span>

      <!-- How many are in a group, since the names alone truncate. -->
      <span
        v-if="group"
        class="tabular shrink-0 text-[0.6875rem] text-muted-foreground"
        :aria-label="$ts('conversation.people', { count: others.length + 1 })"
      >
        {{ others.length + 1 }}
      </span>

      <Icon
        v-if="muted"
        name="lucide:bell-off"
        class="ml-auto size-3 shrink-0 opacity-50"
        :aria-label="$ts('conversation.isMuted')"
      />
      <span
        v-else-if="badge"
        class="motion-badge tabular ml-auto shrink-0 rounded-full bg-primary px-1.5 text-[0.6875rem] font-semibold text-primary-foreground group-hover/row:invisible group-focus-within/row:invisible"
      >
        <UiCount :value="unread!" />
      </span>
    </NuxtLink>

    <!-- A sibling rather than inside the link: a button within a link is two
         controls in one, and neither works properly from the keyboard. -->
    <button
      type="button"
      class="absolute top-1/2 right-1 grid size-6 -translate-y-1/2 cursor-pointer place-items-center rounded-md text-muted-foreground opacity-0 transition-opacity duration-(--motion-duration-fast) group-hover/row:opacity-100 group-focus-within/row:opacity-100 hover:bg-accent hover:text-foreground focus-visible:opacity-100"
      :aria-label="$ts('conversation.optionsFor', { name })"
      aria-haspopup="menu"
      @click="emit('menu', $event.currentTarget as Element)"
    >
      <Icon name="lucide:ellipsis" class="size-3.5" aria-hidden="true" />
    </button>
  </li>
</template>
