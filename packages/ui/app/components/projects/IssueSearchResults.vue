<script setup lang="ts">
import type { IssueSearch } from "@tdc/types";
import SearchExcerpt from "#layers/ui/app/components/SearchExcerpt.vue";
import { whenBriefly } from "#layers/ui/app/lib/dates";

const { $t, $ts, $localeRoute: localeRoute } = useI18n();

/**
 * What a search found: issues, then what was said about issues.
 *
 * Two lists rather than one interleaved: an issue and a comment are different
 * things to open, and somebody looking for "the issue about X" should not
 * have to read past the six comments that mention X to find it.
 */
const props = defineProps<{ results: IssueSearch; showProject?: boolean }>();
</script>

<template>
  <div class="space-y-6">
    <section :aria-label="$ts('projects.search.issuesFound')">
      <h2 class="mb-2 text-xs font-semibold text-muted-foreground uppercase">
        {{ $t("projects.search.issuesFound") }}
        <span class="tabular font-normal">({{ props.results.issues.length }})</span>
      </h2>
      <p v-if="!props.results.issues.length" class="text-sm text-muted-foreground">
        {{ $t("projects.search.noIssues") }}
      </p>
      <ul v-else class="divide-y rounded-lg border bg-surface">
        <li v-for="issue in props.results.issues" :key="issue.id">
          <NuxtLink
            :to="localeRoute({ name: 'issues-ref', params: { ref: issue.key } })"
            class="block px-3 py-2 transition-colors hover:bg-accent"
          >
            <span class="flex items-center gap-2 text-sm">
              <Icon
                v-if="issue.completed"
                name="lucide:circle-check"
                class="size-3.5 shrink-0 text-success-text"
                aria-hidden="true"
              />
              <Icon
                v-else
                name="lucide:circle-dashed"
                class="size-3.5 shrink-0 text-muted-foreground"
                aria-hidden="true"
              />
              <span class="font-mono text-xs text-muted-foreground">{{ issue.key }}</span>
              <span class="min-w-0 flex-1 truncate font-medium">{{ issue.title }}</span>
              <span class="shrink-0 text-xs text-muted-foreground">{{ issue.state }}</span>
            </span>
            <span class="mt-0.5 flex gap-2 text-xs text-muted-foreground">
              <span v-if="props.showProject">{{ issue.projectName }}</span>
              <span>{{
                $t("projects.search.changed", { when: whenBriefly(issue.updatedAt) })
              }}</span>
            </span>
            <SearchExcerpt
              v-if="issue.highlighted"
              :html="issue.highlighted"
              class="mt-1 block text-muted-foreground"
            />
          </NuxtLink>
        </li>
      </ul>
    </section>

    <section
      v-if="props.results.discussions.length"
      :aria-label="$ts('projects.search.discussionsFound')"
    >
      <h2 class="mb-2 text-xs font-semibold text-muted-foreground uppercase">
        {{ $t("projects.search.discussionsFound") }}
        <span class="tabular font-normal">({{ props.results.discussions.length }})</span>
      </h2>
      <ul class="divide-y rounded-lg border bg-surface">
        <li v-for="said in props.results.discussions" :key="said.eventId">
          <NuxtLink
            :to="
              localeRoute({
                name: 'issues-ref',
                params: { ref: said.issueKey },
                query: { at: said.eventId, jump: String(Date.now()) },
              })
            "
            class="block px-3 py-2 transition-colors hover:bg-accent"
          >
            <span class="flex items-center gap-2 text-xs text-muted-foreground">
              <span class="font-mono">{{ said.issueKey }}</span>
              <span class="min-w-0 flex-1 truncate">{{ said.issueTitle }}</span>
              <span>{{ said.sender.split("@")[0] }} · {{ whenBriefly(said.sentAt) }}</span>
            </span>
            <SearchExcerpt :html="said.highlighted" class="mt-1 block" />
          </NuxtLink>
        </li>
      </ul>
    </section>
  </div>
</template>
