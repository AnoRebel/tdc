<script setup lang="ts">
import DOMPurify from "dompurify";

/**
 * A matched message, with the search terms marked.
 *
 * The highlighting comes from Postgres, generated from text the server stored,
 * so the only markup in it should be the marks. It is sanitised anyway, and to
 * a far tighter allowlist than a message body gets: the excerpt has exactly one
 * legitimate tag, so everything else can be dropped rather than reasoned about.
 *
 * Written this way because "the server generated it" is a claim about today's
 * implementation, and a client should not be the layer that depends on it.
 */
const props = defineProps<{ html: string }>();

const safe = computed(() =>
  DOMPurify.sanitize(props.html, {
    ALLOWED_TAGS: ["mark"],
    ALLOWED_ATTR: [],
  }),
);
</script>

<template>
  <!-- eslint-disable-next-line vue/no-v-html -- sanitised above to <mark> alone -->
  <span class="excerpt line-clamp-2 text-sm" v-html="safe" />
</template>

<style scoped>
@reference "~/assets/css/main.css";

.excerpt :deep(mark) {
  @apply rounded bg-primary/20 px-0.5 text-inherit;
}
</style>
