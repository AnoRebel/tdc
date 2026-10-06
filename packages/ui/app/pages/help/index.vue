<script setup lang="ts">
import type { Component } from "vue";
import { Braces, ChevronRight, CodeXml, FileText, Search, ServerCog, Users } from "lucide-vue-next";
import type { SearchableGuide, SectionHit } from "#layers/ui/app/lib/guide-search";
import { GUIDES, guideKey, type GuideGroup } from "#layers/ui/app/lib/guides";
import { Input } from "#layers/ui/app/components/ui/input";

/**
 * The help centre: every guide, grouped by who it is for, and a search across
 * all of them.
 *
 * The guides' text is not here. The index names them from the catalogue, and
 * only a search fetches their content — each guide a chunk of its own, asked
 * for the first time somebody types — so opening Help costs one small page.
 */

definePageMeta({ middleware: "auth", layout: "app" });

const { $t, $ts, $getLocale, $localeRoute: localeRoute } = useI18n();

useHead({ title: $ts("help.title") });

const groups: { id: GuideGroup; icon: Component }[] = [
  { id: "using", icon: Users },
  { id: "operators", icon: ServerCog },
  { id: "developers", icon: CodeXml },
  { id: "templates", icon: FileText },
];

const byGroup = (group: GuideGroup) => GUIDES.filter((guide) => guide.group === group);

const query = ref("");
const searching = ref(false);
const failed = ref(false);
const hits = ref<SectionHit[]>([]);

/*
 * The guides' text, fetched once for searching and kept while the page is open.
 *
 * In the reader's language where a translation exists, so a Swahili reader
 * searching in Swahili finds the Swahili guide — and in English otherwise,
 * because an untranslated guide is still worth finding.
 */
let index: Promise<SearchableGuide[]> | null = null;

function loadIndex(): Promise<SearchableGuide[]> {
  index ??= (async () => {
    const [{ loadGuide }, { guideSections }] = await Promise.all([
      import("#layers/ui/app/lib/guide-content"),
      import("#layers/ui/app/lib/guide-render"),
    ]);
    const locale = $getLocale();
    const loaded = await Promise.all(
      GUIDES.map(async (guide) => {
        const content = await loadGuide(guide.slug, locale);
        if (!content) return null;
        return { slug: guide.slug, ...guideSections(content.source) };
      }),
    );
    return loaded.filter((guide): guide is SearchableGuide => guide !== null);
  })().catch((error: unknown) => {
    // Not cached as a failure: the next keystroke tries again.
    index = null;
    throw error;
  });
  return index;
}

const search = useDebounceFn(async (text: string) => {
  if (!text.trim()) {
    hits.value = [];
    searching.value = false;
    return;
  }
  try {
    const [guides, { searchGuides }] = await Promise.all([
      loadIndex(),
      import("#layers/ui/app/lib/guide-search"),
    ]);
    // Somebody kept typing while the guides arrived; only the latest counts.
    if (text !== query.value) return;
    hits.value = searchGuides(guides, text);
    failed.value = false;
  } catch {
    failed.value = true;
  } finally {
    if (text === query.value) searching.value = false;
  }
}, 150);

watch(query, (text) => {
  searching.value = Boolean(text.trim());
  void search(text);
});

const showingResults = computed(() => Boolean(query.value.trim()));
</script>

<template>
  <div class="flex-1 overflow-y-auto">
    <HelpHeader />

    <div class="mx-auto max-w-4xl px-4 pt-10 pb-16 md:px-8">
      <section class="mb-10">
        <h2 class="text-2xl font-semibold tracking-tight">{{ $t("help.title") }}</h2>
        <p class="mt-1.5 text-sm text-muted-foreground">{{ $t("help.subtitle") }}</p>

        <label class="relative mt-6 block">
          <span class="sr-only">{{ $t("help.searchLabel") }}</span>
          <Search
            class="pointer-events-none absolute top-1/2 z-10 left-3.5 size-4 -translate-y-1/2 text-muted-foreground"
            aria-hidden="true"
          />
          <Input
            v-model="query"
            type="search"
            :placeholder="$ts('help.searchPlaceholder')"
            class="h-11 rounded-lg pl-10 shadow-xs"
          />
        </label>
      </section>

      <!-- Results replace the index while there is something to search for,
           and the index comes back the moment the box is cleared. -->
      <section v-if="showingResults" aria-live="polite">
        <h2 class="sr-only">{{ $t("help.results") }}</h2>
        <p v-if="searching && !hits.length" class="text-sm text-muted-foreground">
          {{ $t("help.searching") }}
        </p>
        <p v-else-if="failed" class="text-sm text-destructive" role="alert">
          {{ $t("help.searchFailed") }}
        </p>
        <p v-else-if="!hits.length" class="text-sm text-muted-foreground">
          {{ $t("help.noResults", { query: query.trim() }) }}
        </p>
        <ul v-else class="divide-y divide-border overflow-hidden rounded-xl border bg-surface">
          <li v-for="hit in hits" :key="`${hit.slug}#${hit.id}`">
            <NuxtLink
              :to="
                localeRoute({
                  name: 'help-slug',
                  params: { slug: hit.slug },
                  hash: hit.id ? `#${hit.id}` : undefined,
                })
              "
              class="block px-4 py-3 transition-colors hover:bg-muted/60 focus-visible:bg-muted/60 focus-visible:outline-none"
            >
              <span class="block text-xs text-muted-foreground">{{ hit.guide }}</span>
              <span class="mt-0.5 block text-sm font-medium">{{ hit.heading }}</span>
              <span class="mt-1 line-clamp-2 block text-sm text-muted-foreground">
                <template v-for="(part, position) in hit.excerpt" :key="position">
                  <mark v-if="part.match" class="rounded bg-primary/20 px-0.5 text-foreground">{{
                    part.text
                  }}</mark>
                  <template v-else>{{ part.text }}</template>
                </template>
              </span>
            </NuxtLink>
          </li>
        </ul>
      </section>

      <template v-else>
        <section v-for="group in groups" :key="group.id" class="mb-10">
          <header class="mb-3 flex items-baseline gap-2">
            <component
              :is="group.icon"
              class="size-4 self-center text-muted-foreground"
              aria-hidden="true"
            />
            <h2 class="text-sm font-semibold">{{ $t(`help.groups.${group.id}`) }}</h2>
            <p class="text-xs text-muted-foreground">{{ $t(`help.groups.${group.id}Hint`) }}</p>
          </header>

          <ul class="grid gap-2 sm:grid-cols-2">
            <li v-for="guide in byGroup(group.id)" :key="guide.slug">
              <NuxtLink
                :to="localeRoute({ name: 'help-slug', params: { slug: guide.slug } })"
                class="group flex h-full items-start gap-3 rounded-xl border bg-surface p-3.5 transition-[border-color,background-color] hover:border-border-strong hover:bg-surface-raised"
              >
                <span
                  class="grid size-8 shrink-0 place-items-center rounded-lg bg-muted text-muted-foreground transition-colors group-hover:bg-primary/10 group-hover:text-primary"
                >
                  <component :is="guide.icon" class="size-4" aria-hidden="true" />
                </span>
                <span class="min-w-0 flex-1">
                  <span class="block text-sm font-medium">
                    {{ $t(`help.guides.${guideKey(guide.slug)}.title`) }}
                  </span>
                  <span class="mt-0.5 block text-xs leading-relaxed text-muted-foreground">
                    {{ $t(`help.guides.${guideKey(guide.slug)}.description`) }}
                  </span>
                </span>
                <ChevronRight
                  class="mt-2 size-4 shrink-0 text-muted-foreground/50 transition-transform group-hover:translate-x-0.5"
                  aria-hidden="true"
                />
              </NuxtLink>
            </li>

            <!-- The API, described by the server itself rather than a guide. -->
            <li v-if="group.id === 'developers'">
              <NuxtLink
                :to="localeRoute({ name: 'help-api' })"
                class="group flex h-full items-start gap-3 rounded-xl border bg-surface p-3.5 transition-[border-color,background-color] hover:border-border-strong hover:bg-surface-raised"
              >
                <span
                  class="grid size-8 shrink-0 place-items-center rounded-lg bg-muted text-muted-foreground transition-colors group-hover:bg-primary/10 group-hover:text-primary"
                >
                  <Braces class="size-4" aria-hidden="true" />
                </span>
                <span class="min-w-0 flex-1">
                  <span class="block text-sm font-medium">{{ $t("help.apiReference.title") }}</span>
                  <span class="mt-0.5 block text-xs leading-relaxed text-muted-foreground">
                    {{ $t("help.apiReference.description") }}
                  </span>
                </span>
                <ChevronRight
                  class="mt-2 size-4 shrink-0 text-muted-foreground/50 transition-transform group-hover:translate-x-0.5"
                  aria-hidden="true"
                />
              </NuxtLink>
            </li>
          </ul>
        </section>

        <section class="rounded-xl border border-dashed p-4">
          <h2 class="mb-2 text-sm font-semibold">{{ $t("help.policies.title") }}</h2>
          <HelpLegalLinks />
        </section>
      </template>
    </div>
  </div>
</template>
