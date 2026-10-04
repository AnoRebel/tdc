<script setup lang="ts">
import type { LinkedEngagement, Project } from "@tdc/types";

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
    name: "board",
    label: $ts("projects.tabs.board"),
    to: localeRoute({ name: "projects-key-board", params: { key: props.project.key } }),
  },
  {
    name: "cycles",
    label: $ts("projects.tabs.cycles"),
    to: localeRoute({ name: "projects-key-cycles", params: { key: props.project.key } }),
  },
  {
    name: "milestones",
    label: $ts("projects.tabs.milestones"),
    to: localeRoute({ name: "projects-key-milestones", params: { key: props.project.key } }),
  },
  {
    name: "progress",
    label: $ts("projects.tabs.progress"),
    to: localeRoute({ name: "projects-key-progress", params: { key: props.project.key } }),
  },
  {
    name: "settings",
    label: $ts("projects.tabs.settings"),
    to: localeRoute({ name: "projects-key-settings", params: { key: props.project.key } }),
  },
]);

/*
 * The engagements this project tracks the work of, for whoever is a party to
 * them — the way back from the work to what was agreed. Everybody else is told
 * of none, by the server, so a member who is not a party learns nothing here.
 */
const { offers } = useServerInfo();
const router = useRouter();
const linked = ref<LinkedEngagement[]>([]);
onMounted(async () => {
  if (!offers("directory") || !router.hasRoute("directory-engagements-id")) return;
  try {
    linked.value = await useDirectory().projectEngagements(props.project.key);
  } catch {
    // Not worth an error over: the project is what this page is about.
  }
});
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
    <p
      v-if="linked.length"
      class="flex flex-wrap items-center gap-x-2 gap-y-1 px-4 pb-1.5 text-xs text-muted-foreground"
    >
      <Icon name="lucide:handshake" class="size-3.5" aria-hidden="true" />
      {{ $ts("directory.engagement.linkedEngagements") }}:
      <NuxtLink
        v-for="engagement in linked"
        :key="engagement.id"
        :to="localeRoute({ name: 'directory-engagements-id', params: { id: engagement.id } })"
        class="font-medium text-foreground hover:underline"
      >
        {{ engagement.title }}
      </NuxtLink>
    </p>
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
