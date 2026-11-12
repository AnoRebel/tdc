<script setup lang="ts">
import type { IssueFromMessage } from "@tdc/types";

const { $ts, $localeRoute: localeRoute } = useI18n();

/**
 * The issues a message became, shown under the message.
 *
 * Only the ones the reader may see — the server leaves the rest out — so a
 * message with nothing here may still have become an issue somewhere the
 * reader cannot look, and nothing here says so.
 */
const props = defineProps<{ issues: IssueFromMessage[] }>();
</script>

<template>
  <ul v-if="props.issues.length" class="mt-1.5 flex flex-wrap gap-1.5">
    <li v-for="issue in props.issues" :key="issue.key">
      <NuxtLink
        :to="localeRoute({ name: 'issues-ref', params: { ref: issue.key } })"
        class="inline-flex max-w-72 items-center gap-1.5 rounded-md border bg-surface px-1.5 py-0.5 text-[0.75rem] text-foreground transition-colors hover:bg-accent"
        :aria-label="
          $ts('projects.fromMessage.trackedAs', { issue: issue.key, title: issue.title })
        "
      >
        <Icon
          v-if="issue.completed"
          name="lucide:circle-check"
          class="size-3 shrink-0 text-success-text"
          aria-hidden="true"
        />
        <Icon
          v-else
          name="lucide:circle-dashed"
          class="size-3 shrink-0 text-muted-foreground"
          aria-hidden="true"
        />
        <span class="font-mono font-semibold">{{ issue.key }}</span>
        <span class="truncate text-muted-foreground">{{ issue.title }}</span>
      </NuxtLink>
    </li>
  </ul>
</template>
