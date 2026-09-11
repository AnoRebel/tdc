<script setup lang="ts">
import { AlertCircle, Code, FileText } from "lucide-vue-next";

/**
 * A text or markdown attachment, read in place.
 *
 * People in a developer community pass logs, patches, configuration and READMEs
 * around constantly, and downloading a file to look at three lines of it is a
 * poor way to spend a connection.
 *
 * The bytes are fetched and rendered here rather than being handed to the
 * browser as a document. That is deliberate: the server serves attachments
 * inertly precisely so a browser never interprets them, and rendering one in a
 * frame would undo that. Plain text goes into a <pre>, which escapes it;
 * markdown goes through a parser with HTML disabled and is then sanitised.
 */

const props = defineProps<{
  src: string;
  filename: string;
  /** "markdown" renders; anything else is shown as plain text. */
  kind: "text" | "markdown";
  size: number;
}>();

/**
 * How much of a file is read.
 *
 * A log can be hundreds of megabytes, and rendering all of it would lock the
 * tab. Enough to read what someone shared, and the rest is a download away.
 */
const maxBytes = 512 * 1024;

const source = ref("");
const error = ref<string | null>(null);
const loading = ref(true);
const truncated = computed(() => props.size > maxBytes);

/** Markdown can be read as its source as well as rendered. */
const showSource = ref(false);
const renderMarkdown = useMarkdown();
const fetchMedia = useMediaFetch();

const rendered = computed(() => {
  if (props.kind !== "markdown" || showSource.value) return "";
  return renderMarkdown(source.value);
});

async function load() {
  loading.value = true;
  error.value = null;

  try {
    // Through the injected fetcher, not fetch: on the desktop the session that
    // authorises this is in the Go client's jar, not the webview, and a fetch
    // from here would be refused. The web client's provider is a credentialed
    // fetch; the desktop's routes through Go.
    const buffer = await fetchMedia(props.src);
    const slice = buffer.byteLength > maxBytes ? buffer.slice(0, maxBytes) : buffer;

    // Anything that is not valid UTF-8 becomes a replacement character rather
    // than throwing: a log with one bad byte should still be readable.
    source.value = new TextDecoder("utf-8", { fatal: false }).decode(slice);
  } catch (cause) {
    // The provider puts the status in the message where it has one; a plain
    // unreachable server has none, and either is more use than a bare failure.
    error.value =
      cause instanceof Error && cause.message ? cause.message : "The file could not be reached.";
  } finally {
    loading.value = false;
  }
}

onMounted(load);
</script>

<template>
  <div class="flex h-full w-full max-w-4xl flex-col overflow-hidden rounded-lg bg-surface">
    <header class="flex shrink-0 items-center gap-2 border-b px-3 py-2">
      <component
        :is="props.kind === 'markdown' ? FileText : Code"
        class="size-3.5 shrink-0 text-muted-foreground"
      />
      <span class="truncate text-xs font-medium">{{ props.filename }}</span>

      <button
        v-if="props.kind === 'markdown'"
        type="button"
        class="btn btn-ghost ml-auto h-6 text-xs"
        @click="showSource = !showSource"
      >
        {{ showSource ? "Rendered" : "Source" }}
      </button>
    </header>

    <div class="min-h-0 flex-1 overflow-auto">
      <div v-if="loading" class="space-y-2 p-4">
        <Skeleton
          v-for="n in 6"
          :key="n"
          class="h-3"
          :style="{ width: `${40 + ((n * 13) % 55)}%` }"
        />
      </div>

      <div v-else-if="error" class="flex items-start gap-2.5 p-4 text-sm">
        <AlertCircle class="mt-0.5 size-4 shrink-0 text-destructive" />
        <span class="text-destructive">{{ error }}</span>
      </div>

      <!-- Rendered markdown. The HTML here has been through a parser with raw
           markup disabled and then a sanitiser; see useMarkdown. -->
      <!-- eslint-disable-next-line vue/no-v-html -->
      <article
        v-else-if="props.kind === 'markdown' && !showSource"
        class="markdown-body p-4"
        v-html="rendered"
      />

      <!-- Plain text, and markdown source. A <pre> escapes its contents, so
           nothing here is interpreted. -->
      <pre v-else class="p-4 font-mono text-xs leading-relaxed break-words whitespace-pre-wrap">{{
        source
      }}</pre>
    </div>

    <footer
      v-if="truncated && !loading && !error"
      class="shrink-0 border-t px-3 py-2 text-[0.6875rem] text-muted-foreground"
    >
      Showing the first {{ Math.round(maxBytes / 1024) }} KB. Download the file to read all of it.
    </footer>
  </div>
</template>

<style scoped>
/*
 * Markdown arrives as plain HTML with no classes to hang utilities on, so its
 * elements are styled here. Scoped and deep, since the content is injected.
 */
.markdown-body :deep(h1),
.markdown-body :deep(h2),
.markdown-body :deep(h3) {
  margin-block: 1.25em 0.5em;
  font-weight: 600;
  line-height: 1.3;
}

.markdown-body :deep(h1) {
  font-size: 1.4em;
}
.markdown-body :deep(h2) {
  font-size: 1.2em;
}
.markdown-body :deep(h3) {
  font-size: 1.05em;
}

.markdown-body :deep(p),
.markdown-body :deep(ul),
.markdown-body :deep(ol),
.markdown-body :deep(blockquote) {
  margin-block: 0.75em;
  /* Prose stays readable when a line does not run the full width. */
  max-width: 68ch;
}

.markdown-body :deep(ul),
.markdown-body :deep(ol) {
  padding-inline-start: 1.5em;
}

.markdown-body :deep(ul) {
  list-style: disc;
}
.markdown-body :deep(ol) {
  list-style: decimal;
}

.markdown-body :deep(li) {
  margin-block: 0.25em;
}

.markdown-body :deep(a) {
  color: var(--primary);
  text-decoration: underline;
  text-underline-offset: 2px;
}

.markdown-body :deep(code) {
  padding: 0.15em 0.35em;
  border-radius: var(--radius-sm);
  background: var(--muted);
  font-family: var(--font-mono);
  font-size: 0.875em;
}

.markdown-body :deep(pre) {
  overflow-x: auto;
  margin-block: 0.75em;
  padding: 0.75em 1em;
  border-radius: var(--radius-md);
  background: var(--surface-sunken);
}

/* A code block sets its own background, so the inline style would double it. */
.markdown-body :deep(pre code) {
  padding: 0;
  background: transparent;
}

.markdown-body :deep(blockquote) {
  padding-inline-start: 1em;
  border-inline-start: 3px solid var(--border-strong);
  color: var(--muted-foreground);
}

.markdown-body :deep(hr) {
  margin-block: 1.5em;
  border: 0;
  border-top: 1px solid var(--border);
}

/* A wide table scrolls inside its own container rather than the page. */
.markdown-body :deep(table) {
  display: block;
  overflow-x: auto;
  margin-block: 0.75em;
  border-collapse: collapse;
  font-size: 0.9em;
}

.markdown-body :deep(th),
.markdown-body :deep(td) {
  padding: 0.4em 0.7em;
  border: 1px solid var(--border);
  text-align: start;
}

.markdown-body :deep(th) {
  background: var(--surface-sunken);
  font-weight: 600;
}

.markdown-body :deep(img) {
  max-width: 100%;
  border-radius: var(--radius-md);
}
</style>
