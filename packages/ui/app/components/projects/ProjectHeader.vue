<script setup lang="ts">
import type { Project } from "@tdc/types";

const { $ts, $localeRoute: localeRoute } = useI18n();

/**
 * A project's name and the ways of looking at it.
 *
 * The tabs are links, not buttons: each view has an address somebody can
 * bookmark or paste into a conversation, and the browser's own back button
 * moves between them.
 */
const props = defineProps<{ project: Project; current: string }>();

const tabs = computed(() => [
  {
    name: "issues",
    label: $ts("projects.tabs.issues"),
    to: localeRoute({ name: "projects-key", params: { key: props.project.key } }),
  },
  {
    name: "settings",
    label: $ts("projects.tabs.settings"),
    to: localeRoute({ name: "projects-key-settings", params: { key: props.project.key } }),
  },
]);
</script>

<template>
  <header class="shrink-0 border-b">
    <div class="flex h-12 items-center gap-2 px-4">
      <NuxtLink
        :to="localeRoute({ name: 'projects' })"
        class="btn btn-ghost size-7 px-0"
        :aria-label="$ts('projects.all')"
      >
        <Icon name="lucide:arrow-left" class="size-4" />
      </NuxtLink>
      <span
        class="rounded bg-muted px-1.5 py-0.5 font-mono text-[0.6875rem] font-semibold text-muted-foreground"
      >
        {{ props.project.key }}
      </span>
      <h1 class="truncate text-sm font-semibold">{{ props.project.name }}</h1>
      <Icon
        v-if="props.project.visibility === 'private'"
        name="lucide:lock"
        class="size-3.5 shrink-0 text-muted-foreground"
        :aria-label="$ts('projects.private')"
      />
      <slot name="actions" />
    </div>
    <nav class="-mb-px flex gap-1 overflow-x-auto px-3" :aria-label="$ts('projects.views')">
      <NuxtLink
        v-for="tab in tabs"
        :key="tab.name"
        :to="tab.to"
        class="border-b-2 px-2.5 py-2 text-xs font-medium whitespace-nowrap transition-colors"
        :class="
          tab.name === props.current
            ? 'border-primary text-foreground'
            : 'border-transparent text-muted-foreground hover:text-foreground'
        "
        :aria-current="tab.name === props.current ? 'page' : undefined"
      >
        {{ tab.label }}
      </NuxtLink>
    </nav>
  </header>
</template>
