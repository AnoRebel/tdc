<script setup lang="ts">
/**
 * A number that says when it changed.
 *
 * The idea is Inspira UI's number ticker, without the library: theirs counts a
 * value up over a second, which is a fine thing on a landing page and the
 * wrong thing on an unread badge — a count somebody has to wait to read is a
 * count they cannot read at a glance.
 *
 * So the number changes at once and the badge acknowledges it. What somebody
 * needs from an unread count is the number, immediately; what the animation
 * adds is noticing that it moved.
 */
const props = withDefaults(defineProps<{ value: number; max?: number }>(), { max: 99 });

/*
 * Past the maximum it stops being a number and becomes "a lot".
 *
 * Nobody acts differently on 147 than on 99, and three digits crowd a badge
 * that has to sit beside an icon.
 */
const shown = computed(() => (props.value > props.max ? `${props.max}+` : String(props.value)));

/** Set briefly when the value rises, which is what the animation keys on. */
const changed = ref(false);
let settle: ReturnType<typeof setTimeout> | undefined;

watch(
  () => props.value,
  (now, before) => {
    // Only upwards. A count going down is somebody reading their messages,
    // which does not need pointing out.
    if (now <= before) return;

    changed.value = true;
    clearTimeout(settle);
    settle = setTimeout(() => (changed.value = false), 400);
  },
);

/*
 * Cleared on unmount.
 *
 * The component that inspired this leaves a loop running after it is gone.
 * A timer is a smaller version of the same mistake and just as avoidable.
 */
onUnmounted(() => clearTimeout(settle));
</script>

<template>
  <span
    class="tabular-nums transition-transform duration-200 motion-reduce:transition-none"
    :class="changed && 'scale-125'"
  >
    {{ shown }}
  </span>
</template>
