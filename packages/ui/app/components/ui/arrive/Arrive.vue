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

    The motion-list preset, from the motion stylesheet, which answers the
    motion preference: under "reduced" a message fades in without rising, and
    under "off" it simply appears. Somebody who asked their system for less
    movement has asked this application too, without a second setting here.
  -->
  <TransitionGroup :tag="tag" appear name="motion-list">
    <slot />
  </TransitionGroup>
</template>
