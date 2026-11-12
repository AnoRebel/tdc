<script setup lang="ts">
import { CircleHelp } from "lucide-vue-next";
/**
 * A way from a screen to the guide that explains it.
 *
 * Small on purpose: it sits beside something somebody is already doing, and a
 * help link that competes with the thing it helps with is noise. The guide and
 * section are named by the screen, so the link lands on the paragraph that
 * answers the question rather than at the top of a long page.
 */
const props = defineProps<{
  /** The guide's slug: "projects", "directory". */
  guide: string;
  /** A section anchor within it, as the guide's own links name it. */
  section?: string;
  /** What the link says. Defaults to "Learn more". */
  label?: string;
}>();

const { $t, $localeRoute: localeRoute } = useI18n();
</script>

<template>
  <NuxtLink
    :to="
      localeRoute({
        name: 'help-slug',
        params: { slug: props.guide },
        hash: props.section ? `#${props.section}` : undefined,
      })
    "
    class="inline-flex items-center gap-1 text-xs text-muted-foreground underline-offset-2 hover:text-foreground hover:underline"
  >
    <CircleHelp class="size-3.5 shrink-0" aria-hidden="true" />
    <span>{{ props.label ?? $t("help.learnMore") }}</span>
  </NuxtLink>
</template>
