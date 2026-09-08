<script setup lang="ts">
/**
 * Something that arrives rather than appears.
 *
 * The idea Inspira UI's animated list is reaching for, kept and made
 * survivable: a message that fades in from slightly below reads as having
 * come from somewhere, where one that materialises in place reads as the
 * interface redrawing itself.
 *
 * CSS transitions rather than an animation library. This is one property over
 * 200ms, and 548 KB of dependency to express that would be a strange trade in
 * an application people open on a phone.
 */
withDefaults(defineProps<{ tag?: string }>(), { tag: "div" });
</script>

<template>
  <!--
    appear, so the first render animates too — otherwise a conversation that
    opens with messages already in it looks different from one that receives
    them, which is the sort of inconsistency people notice without being able
    to say why.

    motion-reduce is honoured throughout: somebody who asked their system for
    less movement has asked this application too.
  -->
  <TransitionGroup
    :tag="tag"
    appear
    enter-active-class="transition duration-200 ease-out motion-reduce:transition-none"
    enter-from-class="translate-y-1 opacity-0"
    leave-active-class="transition duration-150 ease-in motion-reduce:transition-none absolute"
    leave-to-class="opacity-0"
    move-class="transition-transform duration-200 motion-reduce:transition-none"
  >
    <slot />
  </TransitionGroup>
</template>
