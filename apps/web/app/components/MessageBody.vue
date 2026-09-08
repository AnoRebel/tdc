<script setup lang="ts">
import DOMPurify from "dompurify";

import { linkify } from "#layers/ui/app/lib/linkify";

/**
 * A message's text, with its links, addresses and numbers made clickable.
 *
 * Two protections, guarding different things. linkify escapes every character
 * of the body and inserts anchors only around spans it matched itself, so
 * message text cannot become markup. DOMPurify then sanitises the result to
 * anchors alone, which catches a mistake in linkify rather than in the message.
 *
 * The allowlist is deliberately narrow — one tag and three attributes — for
 * the same reason SearchExcerpt's is: there is exactly one thing this markup
 * is allowed to contain, so everything else can be dropped rather than
 * reasoned about.
 */

const props = defineProps<{ body: string }>();

const safe = computed(() =>
  DOMPurify.sanitize(linkify(props.body), {
    ALLOWED_TAGS: ["a"],
    ALLOWED_ATTR: ["href", "target", "rel"],
    // Anchors point away from the application, and target needs rel alongside
    // it. Both are set by linkify; allowing them back is what keeps them.
    ADD_ATTR: ["target", "rel"],
  }),
);
</script>

<template>
  <!-- eslint-disable-next-line vue/no-v-html -- escaped by linkify, then sanitised to anchors above -->
  <span class="message-body" v-html="safe" />
</template>

<style scoped>
@reference "~/assets/css/main.css";

/*
 * Underlined rather than only coloured: inside a sent bubble the text is
 * already on a coloured ground, where a colour change alone is easy to miss
 * and fails contrast guidance on its own.
 */
.message-body :deep(a) {
  @apply underline decoration-current/40 underline-offset-2 transition-colors;
}

.message-body :deep(a:hover) {
  @apply decoration-current;
}
</style>
