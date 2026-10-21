<script setup lang="ts">
/*
 * Replaces the layer's guide page (docus/app/pages/[[lang]]/[...slug].vue) for
 * the links at its foot. The layer asks Nuxt Content for the pages either side,
 * and Content orders them by file name, so "next" after Hosting was whichever
 * file came next alphabetically. Here they are the guides above and below this
 * one in the sidebar (lib/sidebar.ts), crossing into the next group at a
 * group's end.
 *
 * Otherwise the layer's page, less what this site switches off: translations,
 * the AI assistant, its "Edit this page" link and drawn preview images.
 */
import { findPageHeadline } from "@nuxt/content/utils";
import type { ContentNavigationItem } from "@nuxt/content";

import { surround as surroundOf } from "~~/lib/sidebar";

definePageMeta({ layout: "docs" });

const route = useRoute();
const navigation = inject<Ref<ContentNavigationItem[]>>("navigation");

const { data: page } = await useAsyncData(`page-${route.path}`, () =>
  queryCollection("docs").path(route.path).first(),
);

if (!page.value) {
  throw createError({ statusCode: 404, statusMessage: "Page not found", fatal: true });
}

const title = page.value.seo?.title || page.value.title;
// The guide's first paragraph, which keeps the file's line breaks.
const description = (page.value.seo?.description || page.value.description || "")
  .replace(/\s+/g, " ")
  .trim();

const headline = computed(() => findPageHeadline(navigation?.value, page.value?.path));
const breadcrumbs = computed(() => findPageBreadcrumbs(navigation?.value, page.value?.path || ""));
// A missing neighbour is null, as Content's own surround query gives it; the
// component draws nothing in its place, though its type does not say so.
const surround = computed(
  () => surroundOf(navigation?.value ?? [], route.path) as ContentNavigationItem[],
);

useSeo({ title, description, type: "article", breadcrumbs });
useDocsSeo();
</script>

<template>
  <UPage v-if="page">
    <UPageHeader
      :title="page.title"
      :description="page.description"
      :headline="headline"
      :ui="{ wrapper: 'flex-row items-center flex-wrap justify-between' }"
    >
      <template #links>
        <UButton v-for="(link, index) in page.links" :key="index" size="sm" v-bind="link" />
        <DocsPageHeaderLinks />
      </template>
    </UPageHeader>

    <UPageBody>
      <ContentRenderer :value="page" />
      <UContentSurround :surround="surround" />
    </UPageBody>

    <template #right>
      <DocsAsideRight :page="page" />
    </template>
  </UPage>
</template>
