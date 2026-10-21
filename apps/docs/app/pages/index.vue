<script setup lang="ts">
/*
 * The front page: every guide, grouped as the help centre groups them, with
 * the help centre's own names and one-line descriptions (lib/guides.ts).
 */
const appConfig = useAppConfig();
const { tdc } = appConfig as unknown as {
  tdc: {
    guides: { slug: string; group: string; title: string; description: string }[];
    groups: { id: string; title: string; description: string; icon: string }[];
  };
};
const { current } = useDocsVersions();

// The site's own name is the whole title here, rather than "… · TDC docs".
useHead({ titleTemplate: "%s" });
useSeo({ title: "TDC documentation", description: appConfig.seo.description, type: "website" });
useDocsSeo();

const groups = computed(() =>
  tdc.groups
    .map((group) => ({ ...group, guides: tdc.guides.filter((guide) => guide.group === group.id) }))
    .filter((group) => group.guides.length),
);
</script>

<template>
  <UMain>
    <UPageHero
      title="TDC documentation"
      description="TDC is the self-hostable platform of the Tanzania Developers Community: conversations, projects and a directory on a server you run. These guides cover using it, hosting it and building on it."
      :ui="{ container: 'py-16 sm:py-20 lg:py-24' }"
      :links="[
        {
          label: 'Start using TDC',
          to: '/using-tdc',
          icon: 'i-lucide-arrow-right',
          trailing: true,
        },
        { label: 'Host a server', to: '/hosting', color: 'neutral', variant: 'subtle' },
      ]"
    >
      <template #headline>
        <UBadge variant="subtle" color="neutral" class="font-mono"
          >{{ current.label }} · {{ current.version }}</UBadge
        >
      </template>
    </UPageHero>

    <UContainer class="pb-24 space-y-14">
      <p class="-mt-4 hidden lg:flex items-center justify-center gap-2 text-sm text-muted">
        <UKbd value="/" /> to search, <UKbd value="[" /> and <UKbd value="]" /> to move between
        guides, <UKbd value="D" /> for dark mode
      </p>
      <section
        v-for="(group, order) in groups"
        :key="group.id"
        :aria-labelledby="`group-${group.id}`"
        class="tdc-rise"
        :style="{ '--tdc-order': order }"
      >
        <div class="mb-5 flex items-start gap-3">
          <UIcon :name="group.icon" class="mt-1 size-5 text-primary shrink-0" />
          <div>
            <h2 :id="`group-${group.id}`" class="text-xl font-semibold text-highlighted">
              {{ group.title }}
            </h2>
            <p class="text-muted text-sm mt-1">{{ group.description }}</p>
          </div>
        </div>
        <UPageGrid>
          <UPageCard
            v-for="guide in group.guides"
            :key="guide.slug"
            :title="guide.title"
            :description="guide.description"
            :to="`/${guide.slug}`"
            variant="subtle"
            class="tdc-card"
          />
        </UPageGrid>
      </section>
    </UContainer>
  </UMain>
</template>
