<script setup lang="ts">
import DOMPurify from "dompurify";

import { linkify } from "#layers/ui/app/lib/linkify";
import { highlightWithin, renderMessage } from "#layers/ui/app/lib/render-message";

/**
 * A message's text: its formatting rendered, its links made clickable.
 *
 * The body is the Markdown its author typed. Rendering is renderMessage's —
 * a chat-sized subset, raw HTML as text, and a sanitiser over the result;
 * see that file for what is and is not drawn, and why. Rendered bodies are
 * kept, so a conversation redrawing its list does not parse every message
 * again.
 *
 * Somebody who switched formatting off sees the source exactly as typed, with
 * links still found — what every message looked like before formatting
 * existed. That path has two protections of its own: linkify escapes every
 * character and adds only its own anchors, and the sanitiser keeps anchors
 * alone.
 */

const props = defineProps<{ body: string }>();

const { $ts } = useI18n();
const { formatting } = useInterfacePreferences();

const root = ref<HTMLElement | null>(null);

const safe = computed(() =>
  formatting.value
    ? renderMessage(props.body, { copy: $ts("room.formatting.copyCode") })
    : DOMPurify.sanitize(linkify(props.body), {
        ALLOWED_TAGS: ["a"],
        ALLOWED_ATTR: ["href", "target", "rel"],
        ADD_ATTR: ["target", "rel"],
      }),
);

/** Whether the body holds blocks, and so fills its bubble rather than sitting inline. */
const blocks = computed(() => /<(p|ul|ol|blockquote|div)[\s>]/.test(safe.value));

// Colours arrive after the text, and only for a block that names a language.
watch(
  safe,
  () => {
    if (root.value) void highlightWithin(root.value);
  },
  { flush: "post" },
);
onMounted(() => {
  if (root.value) void highlightWithin(root.value);
});

/** Copies a code block's contents, and says so on the button for a moment. */
async function onClick(event: MouseEvent) {
  const button = (event.target as HTMLElement).closest<HTMLButtonElement>("[data-copy-code]");
  if (!button) return;

  const code = button.closest(".code-block")?.querySelector("pre")?.textContent ?? "";
  try {
    await navigator.clipboard?.writeText(code);
  } catch {
    // Refused clipboard access: selecting the text still works.
    return;
  }
  button.textContent = $ts("room.formatting.copied");
  setTimeout(() => (button.textContent = $ts("room.formatting.copyCode")), 1500);
}
</script>

<template>
  <!-- The click is delegated to the copy buttons inside, which are real buttons
       and so reach the keyboard themselves. -->
  <!-- eslint-disable-next-line vue/no-v-html -- rendered then sanitised above -->
  <span
    ref="root"
    class="message-body"
    :class="{ 'message-blocks': blocks }"
    v-html="safe"
    @click="onClick"
  />
</template>

<style scoped>
@reference "#layers/ui/app/assets/css/main.css";

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

/*
 * Formatting.
 *
 * Inline styles take the colour of the bubble they are in, so they read on a
 * sent message's saturated ground as well as on a received one's. A code
 * block is the exception: it brings its own surface, so syntax colours have
 * one known background to be legible against in each theme.
 *
 * The body is a span so that a one-line message sits beside its "edited"
 * marker as it always has; a body with blocks in it fills the bubble.
 */
.message-blocks {
  display: block;
}

.message-body :deep(p + p),
.message-body :deep(p + ul),
.message-body :deep(p + ol),
.message-body :deep(p + blockquote),
.message-body :deep(p + .code-block),
.message-body :deep(ul + *),
.message-body :deep(ol + *),
.message-body :deep(blockquote + *),
.message-body :deep(.code-block + *) {
  margin-top: 0.4em;
}

.message-body :deep(strong) {
  font-weight: 600;
}

.message-body :deep(ul),
.message-body :deep(ol) {
  padding-inline-start: 1.25em;
  white-space: normal;
}
.message-body :deep(ul) {
  list-style: disc;
}
.message-body :deep(ol) {
  list-style: decimal;
}
.message-body :deep(li) {
  white-space: pre-wrap;
}

.message-body :deep(blockquote) {
  padding-inline-start: 0.6em;
  border-inline-start: 3px solid color-mix(in oklab, currentColor 35%, transparent);
}

.message-body :deep(:not(pre) > code) {
  padding: 0.1em 0.3em;
  border-radius: var(--radius-sm);
  background: color-mix(in oklab, currentColor 12%, transparent);
  font-family: var(--font-mono);
  font-size: 0.875em;
}

.message-body :deep(.code-block) {
  overflow: hidden;
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  background: var(--surface-sunken);
  color: var(--foreground);
}

.message-body :deep(.code-block-bar) {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.5rem;
  padding: 0.15rem 0.25rem 0.15rem 0.6rem;
  border-bottom: 1px solid var(--border);
  color: var(--muted-foreground);
  font-family: var(--font-mono);
  font-size: 0.6875rem;
  white-space: nowrap;
}

.message-body :deep(.code-copy) {
  cursor: pointer;
  padding: 0.1rem 0.4rem;
  border-radius: var(--radius-sm);
  font-family: var(--font-sans);
}
.message-body :deep(.code-copy:hover) {
  background: var(--accent);
  color: var(--accent-foreground);
}
.message-body :deep(.code-copy:focus-visible) {
  outline: 2px solid var(--ring);
}

/* A long line scrolls inside the block rather than widening the conversation. */
.message-body :deep(pre) {
  overflow-x: auto;
  max-width: 100%;
  margin: 0;
  padding: 0.5rem 0.6rem;
  font-family: var(--font-mono);
  font-size: 0.8125rem;
  line-height: 1.5;
  white-space: pre;
}
.message-body :deep(pre:focus-visible) {
  outline: 2px solid var(--ring);
  outline-offset: -2px;
}

/*
 * Syntax colours, one set per theme (below), each chosen to clear 4.5:1
 * against the code block's own surface.
 */
.message-body :deep(.shj-syn-kwd),
.message-body :deep(.shj-syn-err),
.message-body :deep(.shj-syn-deleted) {
  color: var(--syntax-keyword);
}
.message-body :deep(.shj-syn-str),
.message-body :deep(.shj-syn-insert) {
  color: var(--syntax-string);
}
.message-body :deep(.shj-syn-num),
.message-body :deep(.shj-syn-bool),
.message-body :deep(.shj-syn-type),
.message-body :deep(.shj-syn-var),
.message-body :deep(.shj-syn-section) {
  color: var(--syntax-literal);
}
.message-body :deep(.shj-syn-func),
.message-body :deep(.shj-syn-class) {
  color: var(--syntax-function);
}
.message-body :deep(.shj-syn-cmnt) {
  color: var(--muted-foreground);
  font-style: italic;
}
</style>

<style>
/*
 * A bubble holding a code block takes the bubble's full width, and may give
 * it up to fit beside the message's buttons.
 *
 * A bubble otherwise sizes to its text, which can always wrap to fit. A code
 * block cannot: its longest line is the narrowest it will go, so a bubble
 * sized to its content would push past the edge of a phone rather than let
 * the block scroll. A width that does not depend on the content — the most a
 * bubble may have, never more than its row allows — is what lets the block
 * scroll inside it instead.
 */
[data-slot="bubble"]:has(.code-block) {
  width: min(42rem, 68vw);
  max-width: 100%;
}
.flex-col:has(> [data-slot="bubble"] .code-block) {
  min-width: 0;
}

.message-body {
  --syntax-keyword: #b3261e;
  --syntax-string: #116329;
  --syntax-literal: #0550ae;
  --syntax-function: #7a3db8;
}

.dark .message-body {
  --syntax-keyword: #ff8f86;
  --syntax-string: #7ee2a0;
  --syntax-literal: #8cc8ff;
  --syntax-function: #d2a8ff;
}
</style>
