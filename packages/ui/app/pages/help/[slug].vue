<script setup lang="ts">
import {
  ArrowLeft,
  BookX,
  ChevronDown,
  Clock,
  FileCode,
  Languages,
  List,
  TriangleAlert,
} from "lucide-vue-next";
import type { RenderedGuide } from "#layers/ui/app/lib/guide-render";
import { guideFor, guideKey, SOURCE_REPOSITORY } from "#layers/ui/app/lib/guides";
import { Button } from "#layers/ui/app/components/ui/button";

/**
 * One guide, read inside the application.
 *
 * The text is docs/<slug>.md itself, fetched as a chunk of its own when this
 * page opens, and the parser and sanitiser arrive with it rather than with the
 * application: somebody who never opens Help never downloads either.
 *
 * Links between guides stay inside the application, and a link to a section
 * scrolls this page rather than reloading it — see guides.ts for how a link
 * written for a repository page is resolved here.
 */

definePageMeta({ middleware: "auth", layout: "app" });

const { $t, $ts, $getLocale, $localeRoute: localeRoute } = useI18n();
const route = useRoute();
const router = useRouter();

const slug = computed(() => String(route.params.slug ?? ""));
const guide = computed(() => guideFor(slug.value));

type State =
  | { status: "loading" }
  | { status: "missing" }
  | { status: "failed" }
  | { status: "ready"; rendered: RenderedGuide; translated: boolean; minutes: number };

const state = ref<State>({ status: "loading" });
const scroller = ref<HTMLElement | null>(null);
const active = ref("");

const title = computed(() =>
  guide.value ? $ts(`help.guides.${guideKey(guide.value.slug)}.title`) : $ts("help.title"),
);
useHead({ title });

/** Whether somebody reading in another language is being shown English. */
const untranslated = computed(
  () => state.value.status === "ready" && !state.value.translated && $getLocale() !== "en",
);

let generation = 0;

async function load() {
  const asked = ++generation;
  if (!guide.value) {
    state.value = { status: "missing" };
    return;
  }

  state.value = { status: "loading" };
  try {
    const [{ loadGuide }, { renderGuide }] = await Promise.all([
      import("#layers/ui/app/lib/guide-content"),
      import("#layers/ui/app/lib/guide-render"),
    ]);
    const content = await loadGuide(slug.value, $getLocale());
    if (asked !== generation) return;
    if (!content) {
      state.value = { status: "missing" };
      return;
    }

    const rendered = renderGuide(content.source, {
      copy: $ts("help.copy"),
      anchor: $ts("help.anchor"),
    });
    const words = content.source.split(/\s+/).length;
    state.value = {
      status: "ready",
      rendered,
      translated: content.translated,
      minutes: Math.max(1, Math.round(words / 220)),
    };

    await nextTick();
    if (route.hash) scrollToSection(route.hash.slice(1), "instant");
    else scroller.value?.scrollTo({ top: 0 });
    trackActive();
  } catch {
    if (asked === generation) state.value = { status: "failed" };
  }
}

watch(slug, () => void load(), { immediate: true });

// A section chosen from the contents, a link, or the browser's own history.
watch(
  () => route.hash,
  (hash) => {
    if (state.value.status === "ready" && hash) scrollToSection(hash.slice(1), "smooth");
  },
);

function headingFor(id: string): HTMLElement | null {
  return scroller.value?.querySelector<HTMLElement>(`#${CSS.escape(`guide-${id}`)}`) ?? null;
}

function scrollToSection(id: string, behavior: ScrollBehavior) {
  let decoded = id;
  try {
    decoded = decodeURIComponent(id);
  } catch {
    // Already plain.
  }
  headingFor(decoded)?.scrollIntoView({ behavior, block: "start" });
  active.value = decoded;
}

function goToSection(id: string) {
  void router.replace(
    localeRoute({ name: "help-slug", params: { slug: slug.value }, hash: `#${id}` }),
  );
  // The same hash twice is not a change the watcher sees, so scroll anyway.
  if (route.hash === `#${id}`) scrollToSection(id, "smooth");
}

/*
 * The section being read, for the contents to mark.
 *
 * The last heading that has reached the top of the page — measured on scroll
 * rather than observed, because the answer is about the order of several
 * headings and an observer reports on each one separately.
 */
function trackActive() {
  if (state.value.status !== "ready" || !scroller.value) return;
  const top = scroller.value.getBoundingClientRect().top + 96;
  let current = "";
  for (const entry of state.value.rendered.toc) {
    const heading = headingFor(entry.id);
    if (heading && heading.getBoundingClientRect().top <= top) current = entry.id;
  }
  active.value = current;
}

useEventListener(scroller, "scroll", useThrottleFn(trackActive, 100), { passive: true });

/*
 * What a click inside the guide means.
 *
 * Handled once, for the whole article, because the article is markup rather
 * than components: a link to another guide becomes a navigation inside the
 * application, a link to a section scrolls, and a copy button copies the code
 * beside it. A click with a modifier is left to the browser, so opening a
 * guide in a new tab still works.
 */
async function onArticleClick(event: MouseEvent) {
  const target = event.target as HTMLElement | null;

  const copy = target?.closest<HTMLButtonElement>("button[data-copy]");
  if (copy) {
    const code = copy.closest(".guide-code")?.querySelector("code")?.textContent ?? "";
    try {
      await navigator.clipboard.writeText(code);
      copy.textContent = $ts("help.copied");
      copy.dataset.copied = "";
      setTimeout(() => {
        copy.textContent = $ts("help.copy");
        delete copy.dataset.copied;
      }, 1500);
    } catch {
      // No clipboard, or permission refused. The code is on screen to select.
    }
    return;
  }

  const link = target?.closest<HTMLAnchorElement>("a[data-section], a[data-guide]");
  if (!link || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
  event.preventDefault();

  const section = link.dataset.section ?? "";
  const other = link.dataset.guide;
  if (other && other !== slug.value) {
    await router.push(
      localeRoute({
        name: "help-slug",
        params: { slug: other },
        hash: section ? `#${section}` : undefined,
      }),
    );
    return;
  }
  if (section) goToSection(section);
}

const article = ref<HTMLElement | null>(null);
useEventListener(article, "click", (event: MouseEvent) => void onArticleClick(event));

const sourceUrl = computed(() => `${SOURCE_REPOSITORY}docs/${slug.value}.md`);
</script>

<template>
  <div ref="scroller" class="flex-1 overflow-y-auto">
    <HelpHeader :current="title">
      <a
        v-if="guide"
        :href="sourceUrl"
        target="_blank"
        rel="noopener noreferrer"
        class="btn btn-ghost h-7 gap-1.5 px-2 text-xs text-muted-foreground"
      >
        <FileCode class="size-3.5" aria-hidden="true" />
        <span class="max-sm:sr-only">{{ $t("help.viewSource") }}</span>
      </a>
    </HelpHeader>

    <div
      v-if="state.status === 'missing' || state.status === 'failed'"
      class="mx-auto flex max-w-md flex-col items-center gap-3 px-4 py-24 text-center"
    >
      <span class="grid size-10 place-items-center rounded-full bg-muted text-muted-foreground">
        <BookX class="size-5" aria-hidden="true" />
      </span>
      <p class="text-sm text-muted-foreground" role="alert">
        {{ state.status === "missing" ? $t("help.notFound") : $t("help.loadFailed") }}
      </p>
      <div class="flex gap-2">
        <Button v-if="state.status === 'failed'" variant="outline" size="small" @click="load">
          {{ $t("help.retry") }}
        </Button>
        <NuxtLink :to="localeRoute({ name: 'help' })" class="btn btn-ghost h-8 px-3 text-sm">
          {{ $t("help.allGuides") }}
        </NuxtLink>
      </div>
    </div>

    <div v-else class="mx-auto flex max-w-6xl gap-10 px-4 pt-8 pb-24 md:px-8">
      <article class="min-w-0 flex-1" :aria-busy="state.status === 'loading'">
        <div class="mx-auto max-w-[46rem]">
          <!-- Loading: the shape of a page, so nothing jumps when it arrives. -->
          <div v-if="state.status === 'loading'" class="space-y-4" aria-hidden="true">
            <div class="h-9 w-2/3 animate-pulse rounded-md bg-muted" />
            <div class="h-4 w-32 animate-pulse rounded bg-muted" />
            <div
              v-for="line in 8"
              :key="line"
              class="h-4 animate-pulse rounded bg-muted"
              :class="line % 3 ? 'w-full' : 'w-4/5'"
            />
          </div>

          <template v-else-if="state.status === 'ready'">
            <header class="mb-8 border-b pb-6">
              <h1 class="text-3xl font-semibold tracking-tight text-balance">
                {{ state.rendered.title || title }}
              </h1>
              <p class="mt-2 flex items-center gap-1.5 text-xs text-muted-foreground">
                <Clock class="size-3.5" aria-hidden="true" />
                {{ $t("help.minutes", { count: state.minutes }) }}
              </p>

              <p
                v-if="untranslated"
                class="mt-4 flex gap-2 rounded-lg border border-border bg-muted/50 px-3 py-2.5 text-sm"
              >
                <Languages
                  class="mt-0.5 size-4 shrink-0 text-muted-foreground"
                  aria-hidden="true"
                />
                <span>{{ $t("help.notTranslated") }}</span>
              </p>
              <p
                v-if="guide?.group === 'templates'"
                class="mt-4 flex gap-2 rounded-lg border border-warning/40 bg-warning/10 px-3 py-2.5 text-sm"
              >
                <TriangleAlert class="mt-0.5 size-4 shrink-0 text-warning" aria-hidden="true" />
                <span>{{ $t("help.templateNote") }}</span>
              </p>
            </header>

            <!-- On a narrow screen the contents fold away above the guide. -->
            <details
              v-if="state.rendered.toc.length"
              class="group mb-8 rounded-lg border bg-surface xl:hidden"
            >
              <summary
                class="flex cursor-pointer list-none items-center gap-2 px-3 py-2 text-sm font-medium"
              >
                <List class="size-4 text-muted-foreground" aria-hidden="true" />
                {{ $t("help.onThisPage") }}
                <ChevronDown
                  class="ml-auto size-4 text-muted-foreground transition-transform group-open:rotate-180"
                  aria-hidden="true"
                />
              </summary>
              <ol class="max-h-72 overflow-y-auto border-t px-1 py-1.5 text-sm">
                <li v-for="entry in state.rendered.toc" :key="entry.id">
                  <a
                    :href="`#${entry.id}`"
                    class="block rounded px-2 py-1 text-muted-foreground hover:bg-muted hover:text-foreground"
                    :class="entry.depth === 3 && 'pl-5 text-xs'"
                    @click.prevent="goToSection(entry.id)"
                  >
                    {{ entry.text }}
                  </a>
                </li>
              </ol>
            </details>

            <!-- eslint-disable-next-line vue/no-v-html -- rendered from docs/ with raw HTML escaped, then sanitised; see guide-render.ts -->
            <div ref="article" class="guide" v-html="state.rendered.html" />

            <footer class="mt-16 flex flex-wrap items-center justify-between gap-4 border-t pt-6">
              <NuxtLink
                :to="localeRoute({ name: 'help' })"
                class="inline-flex items-center gap-1.5 text-sm text-muted-foreground hover:text-foreground"
              >
                <ArrowLeft class="size-4" aria-hidden="true" />
                {{ $t("help.allGuides") }}
              </NuxtLink>
              <HelpLegalLinks compact />
            </footer>
          </template>
        </div>
      </article>

      <!-- The contents, beside the guide where there is room for both. -->
      <aside
        v-if="state.status === 'ready' && state.rendered.toc.length"
        class="sticky top-20 hidden max-h-[calc(100dvh-7rem)] w-56 shrink-0 self-start overflow-y-auto xl:block"
      >
        <h2 class="mb-2 text-xs font-semibold tracking-wide text-muted-foreground uppercase">
          {{ $t("help.onThisPage") }}
        </h2>
        <ol class="space-y-px border-l text-[0.8125rem]">
          <li v-for="entry in state.rendered.toc" :key="entry.id">
            <a
              :href="`#${entry.id}`"
              class="-ml-px block border-l py-1 leading-snug transition-colors"
              :class="[
                entry.depth === 3 ? 'pl-6' : 'pl-3',
                active === entry.id
                  ? 'border-primary font-medium text-foreground'
                  : 'border-transparent text-muted-foreground hover:text-foreground',
              ]"
              :aria-current="active === entry.id ? 'location' : undefined"
              @click.prevent="goToSection(entry.id)"
            >
              {{ entry.text }}
            </a>
          </li>
        </ol>
      </aside>
    </div>
  </div>
</template>

<style scoped>
@reference "#layers/ui/app/assets/css/main.css";

/*
 * Long-form reading, which nothing else in the application does.
 *
 * A measure of about seventy characters, a looser line height than chat, and
 * headings with room above them so a section reads as a new start. Styled here
 * rather than with a typography plugin, because these are the only pages that
 * need it and the rules are few.
 */
.guide {
  @apply text-[0.9375rem] leading-7 text-foreground/90;
}

.guide :deep(h2) {
  @apply mt-12 mb-3 scroll-mt-20 border-b pb-2 text-xl font-semibold tracking-tight text-foreground;
}
.guide :deep(h3) {
  @apply mt-8 mb-2 scroll-mt-20 text-base font-semibold text-foreground;
}
.guide :deep(h4) {
  @apply mt-6 mb-2 scroll-mt-20 text-sm font-semibold text-foreground;
}
.guide :deep(.guide-anchor) {
  @apply ml-1 font-normal text-muted-foreground/0 no-underline transition-colors;
}
.guide :deep(:is(h2, h3, h4):hover .guide-anchor),
.guide :deep(.guide-anchor:focus-visible) {
  @apply text-muted-foreground;
}

.guide :deep(p),
.guide :deep(ul),
.guide :deep(ol),
.guide :deep(blockquote) {
  @apply my-4;
}
.guide :deep(ul) {
  @apply list-disc pl-6;
}
.guide :deep(ol) {
  @apply list-decimal pl-6;
}
.guide :deep(li) {
  @apply my-1 pl-1;
}
.guide :deep(li > p) {
  @apply my-1;
}
.guide :deep(strong) {
  @apply font-semibold text-foreground;
}
.guide :deep(a) {
  @apply font-medium text-primary underline decoration-primary/30 underline-offset-2 hover:decoration-primary;
}
.guide :deep(blockquote) {
  @apply border-l-2 border-primary/40 pl-4 text-muted-foreground;
}
.guide :deep(hr) {
  @apply my-10 border-border;
}

.guide :deep(:not(pre) > code) {
  @apply rounded border border-border/60 bg-muted px-1 py-px font-mono text-[0.8125em] text-foreground;
}

.guide :deep(.guide-code) {
  @apply my-5 overflow-hidden rounded-lg border bg-surface-sunken;
}
.guide :deep(.guide-code-bar) {
  @apply flex h-8 items-center justify-between border-b px-3 font-mono text-[0.6875rem] text-muted-foreground;
}
.guide :deep(.guide-copy) {
  @apply cursor-pointer rounded px-1.5 py-0.5 font-sans text-[0.6875rem] text-muted-foreground transition-colors hover:bg-muted hover:text-foreground;
}
.guide :deep(.guide-copy[data-copied]) {
  @apply text-success-text;
}
.guide :deep(pre) {
  @apply overflow-x-auto p-3.5 font-mono text-[0.8125rem] leading-6;
}

.guide :deep(.guide-table) {
  @apply my-5 overflow-x-auto rounded-lg border;
}
.guide :deep(table) {
  @apply w-full border-collapse text-sm;
}
.guide :deep(th) {
  @apply border-b bg-muted/50 px-3 py-2 text-left font-semibold text-foreground;
}
.guide :deep(td) {
  @apply border-b px-3 py-2 align-top;
}
.guide :deep(tr:last-child td) {
  @apply border-b-0;
}
</style>
