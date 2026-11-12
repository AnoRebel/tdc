<script setup lang="ts">
import { Spinner } from "#layers/ui/app/components/ui/spinner";

/**
 * A message's text, translated when this reader wants it to be.
 *
 * The translation is Markdown like the original — code, mentions and links
 * came back exactly as written — so it is drawn by the same MessageBody, and
 * looks like the message rather than like a quotation of it.
 *
 * Below it, one quiet line says what happened: that it was translated and from
 * what, with the original a press away. A reader must always be able to tell a
 * machine's words from the author's, and get back to the author's.
 */

const props = defineProps<{
  body: string;
  messageId: string;
  roomId: string;
  mine?: boolean;
}>();

const { $t, $ts, $getLocale } = useI18n();
const translation = useTranslation();

const translating = computed(() =>
  translation.showsTranslation(props.messageId, props.roomId, Boolean(props.mine)),
);

const entry = computed(() =>
  translating.value ? translation.entry(props.messageId, props.body) : undefined,
);

// Asked for as soon as it is wanted: by a rule, or by the reader's press.
watchEffect(() => {
  if (translating.value && !entry.value) {
    translation.request(props.roomId, props.messageId, props.body);
  }
});

const shown = computed(() =>
  entry.value?.state === "done" && !entry.value.skipped && entry.value.body
    ? entry.value.body
    : props.body,
);

/** A language's name in the reader's own language, or its code where unknown. */
function languageName(code: string | undefined): string {
  if (!code) return "";
  try {
    return new Intl.DisplayNames([$getLocale()], { type: "language" }).of(code) ?? code;
  } catch {
    return code;
  }
}

const failure = computed(() => {
  switch (entry.value?.failure) {
    case "needs-click":
      return $ts("translation.needsClick");
    case "rate-limited":
      return $ts("translation.rateLimited");
    case "not-connected":
      return $ts("translation.notConnected");
    case "unsupported":
      return $ts("translation.unsupported");
    default:
      return $ts("translation.failed");
  }
});

function showOriginal() {
  translation.toggle(props.roomId, props.messageId, props.body, Boolean(props.mine));
}

function retry() {
  translation.retry(props.roomId, props.messageId, props.body);
}
</script>

<template>
  <MessageBody :body="shown" />
  <Transition name="translation-note">
    <span
      v-if="
        translating &&
        entry &&
        !(entry.state === 'done' && entry.skipped && translation.chosenFor(messageId) === undefined)
      "
      class="translation-note mt-1 flex flex-wrap items-center gap-x-1.5 text-[0.6875rem] text-muted-foreground"
      role="status"
      aria-live="polite"
    >
      <template v-if="entry.state === 'pending'">
        <Spinner size="sm" />
        {{ $t("translation.translating") }}
      </template>
      <template v-else-if="entry.state === 'done' && entry.skipped">
        {{ $t("translation.alreadyIn", { language: languageName(translation.target.value) }) }}
      </template>
      <template v-else-if="entry.state === 'done'">
        <span>
          {{
            entry.sourceLanguage
              ? $t("translation.translatedFrom", { language: languageName(entry.sourceLanguage) })
              : $t("translation.translated")
          }}
        </span>
        <span aria-hidden="true">·</span>
        <button type="button" class="translation-link" @click="showOriginal">
          {{ $t("translation.showOriginal") }}
        </button>
      </template>
      <template v-else>
        <span>{{ failure }}</span>
        <span aria-hidden="true">·</span>
        <button type="button" class="translation-link" @click="retry">
          {{ $t("translation.tryAgain") }}
        </button>
      </template>
    </span>
  </Transition>
</template>

<style scoped>
.translation-note {
  white-space: normal;
}

/* A link in look, a button in behaviour: it changes what is shown, not where. */
.translation-link {
  cursor: pointer;
  border-radius: var(--radius-sm);
  text-decoration: underline;
  text-decoration-color: color-mix(in oklab, currentColor 40%, transparent);
  text-underline-offset: 2px;
}
.translation-link:hover {
  color: var(--foreground);
  text-decoration-color: currentColor;
}
.translation-link:focus-visible {
  outline: 2px solid var(--ring);
  outline-offset: 1px;
}

/* A fade, so it reads the same under reduced motion: nothing moves. */
.translation-note-enter-active,
.translation-note-leave-active {
  transition: opacity 150ms ease;
}
.translation-note-enter-from,
.translation-note-leave-to {
  opacity: 0;
}
</style>
