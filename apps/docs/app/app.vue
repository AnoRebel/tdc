<script setup lang="ts">
/*
 * Replaces the layer's app.vue for one reason: the sidebar. The guides are
 * flat files in docs/, so the layer would list them alphabetically; this
 * groups and orders them the way the application's help centre does, from the
 * same catalogue (lib/guides.ts). Internationalisation and the AI assistant
 * the layer's version also wires up are not used here.
 */
import type { ContentNavigationItem } from "@nuxt/content";

const appConfig = useAppConfig();
const { seo } = appConfig;
const site = useSiteConfig();
const { tdc } = appConfig as unknown as {
  tdc: {
    guides: { slug: string; group: string; title: string }[];
    groups: { id: string; title: string; icon: string }[];
  };
};

useDocusShortcuts();

useHead({
  meta: [{ name: "viewport", content: "width=device-width, initial-scale=1" }],
  htmlAttrs: { lang: "en" },
});

useSeoMeta({
  titleTemplate: seo.titleTemplate,
  title: seo.title,
  description: seo.description,
  ogSiteName: site.name,
  twitterCard: "summary_large_image",
});

function grouped(pages: ContentNavigationItem[]): ContentNavigationItem[] {
  const byPath = new Map(pages.map((page) => [page.path, page]));
  const placed = new Set<string>();

  const groups = tdc.groups.map((group) => ({
    title: group.title,
    icon: group.icon,
    path: `/${group.id}`,
    children: tdc.guides
      .filter((guide) => guide.group === group.id)
      .flatMap((guide) => {
        const page = byPath.get(`/${guide.slug}`);
        if (!page) return [];
        placed.add(page.path);
        return [{ ...page, title: guide.title }];
      }),
  }));

  // A file in docs/ the help centre does not list yet still has a place here.
  const rest = pages.filter((page) => !placed.has(page.path));
  if (rest.length)
    groups.push({ title: "More", icon: "i-lucide-files", path: "/more", children: rest });

  return groups.filter((group) => group.children.length);
}

const { data: navigation } = await useAsyncData(
  "navigation_docs",
  () => queryCollectionNavigation("docs"),
  {
    transform: grouped,
  },
);

provide("navigation", navigation);

/*
 * Keys, beside the layer's own (Ctrl/⌘+K to search, D for dark mode):
 * "/" searches too, "[" and "]" step through the guides in sidebar order, and
 * G then H goes to the front page. None of them fires while typing in a field.
 */
const route = useRoute();
const { open: openSearch } = useContentSearch();
const order = computed(() => (navigation.value ?? []).flatMap((group) => group.children ?? []));

function step(by: number) {
  const index = order.value.findIndex((page) => page.path === route.path);
  const target = order.value[index === -1 ? 0 : index + by];
  if (target) navigateTo(target.path);
}

defineShortcuts({
  "/": () => (openSearch.value = true),
  "[": () => step(-1),
  "]": () => step(1),
  "g-h": () => navigateTo("/"),
});
</script>

<template>
  <UApp>
    <NuxtLoadingIndicator color="var(--ui-primary)" />
    <VersionBanner />
    <AppHeader />
    <NuxtLayout>
      <NuxtPage />
    </NuxtLayout>
    <AppFooter />

    <ClientOnly>
      <AppSearch :navigation="navigation" />
    </ClientOnly>
  </UApp>
</template>
