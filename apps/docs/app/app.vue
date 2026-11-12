<script setup lang="ts">
/*
 * Replaces the layer's app.vue for one reason: the sidebar. The guides are
 * flat files in docs/, so the layer would list them alphabetically; this
 * groups and orders them the way the application's help centre does, from the
 * same catalogue (lib/sidebar.ts). Internationalisation and the AI assistant
 * the layer's version also wires up are not used here.
 */
import { readingOrder, sidebar, type SidebarCatalogue } from "~~/lib/sidebar";

const appConfig = useAppConfig();
const { seo } = appConfig;
const site = useSiteConfig();
const { tdc } = appConfig as unknown as { tdc: SidebarCatalogue };

useDocusShortcuts();

useHead({
  meta: [{ name: "viewport", content: "width=device-width, initial-scale=1" }],
  htmlAttrs: { lang: "en" },
});

useSeoMeta({
  titleTemplate: "%s · TDC docs",
  title: seo.title,
  description: seo.description,
  ogSiteName: site.name,
  twitterCard: "summary_large_image",
});

const { data: navigation } = await useAsyncData(
  "navigation_docs",
  () => queryCollectionNavigation("docs"),
  {
    transform: (pages) => sidebar(pages, tdc),
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
const order = computed(() => readingOrder(navigation.value ?? []));

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
