<script setup lang="ts">
import type { IssueHistoryEntry } from "@tdc/types";
import { fully, whenBriefly } from "#layers/ui/app/lib/dates";

const { $ts } = useI18n();

/**
 * What happened to an issue, oldest first.
 *
 * Read from the project's event log and shown as it was recorded: the names a
 * state had when the issue moved, not the names it has now, because the
 * history is of what happened and a state renamed later did not change that.
 *
 * Nothing here edits anything. There is no control on an entry, because the
 * server has no way to change one.
 */
const props = defineProps<{ entries: IssueHistoryEntry[] }>();

/** The person, without the server when it is this one's. */
function who(address: string): string {
  // No person in it: something the server did by itself, on schedule.
  if (!address.includes("@")) return $ts("projects.history.server");
  return address.split("@")[0] ?? address;
}

function text(value: unknown): string {
  return typeof value === "string" ? value : String(value ?? "");
}

/** One entry, said as a sentence. */
function describe(entry: IssueHistoryEntry): string {
  const actor = who(entry.actor);
  const content = entry.content;

  switch (entry.type) {
    case "issue.created":
      return $ts("projects.history.created", { actor });
    case "issue.state_changed":
      return $ts("projects.history.moved", {
        actor,
        from: text(content.previousName),
        to: text(content.newName),
      });
    case "issue.title_changed":
      return $ts("projects.history.renamed", {
        actor,
        from: text(content.previous),
        to: text(content.new),
      });
    case "issue.description_changed":
      return $ts("projects.history.described", { actor });
    case "issue.deleted":
      return $ts("projects.history.deleted", { actor });
    case "issue.assigned":
      return $ts("projects.history.assigned", { actor, who: who(text(content.assignee)) });
    case "issue.unassigned":
      return $ts("projects.history.unassigned", { actor, who: who(text(content.assignee)) });
    case "issue.labeled":
      return $ts("projects.history.labeled", { actor, label: text(content.labelName) });
    case "issue.unlabeled":
      return $ts("projects.history.unlabeled", { actor, label: text(content.labelName) });
    case "issue.priority_changed":
      return $ts("projects.history.prioritised", {
        actor,
        to: text(content.newName) || $ts("projects.priority.none"),
      });
    case "issue.related":
      return $ts("projects.history.related", {
        actor,
        kind: $ts(`projects.relation.${text(content.kind)}`),
        other: text(content.otherKey),
      });
    case "issue.unrelated":
      return $ts("projects.history.unrelated", {
        actor,
        kind: $ts(`projects.relation.${text(content.kind)}`),
        other: text(content.otherKey),
      });
    case "issue.cycle_changed":
      // Done by the server itself when a cycle ended, under the project's policy.
      if (content.reason === "cycle_ended") {
        return content.newName
          ? $ts("projects.history.carried", {
              from: text(content.previousName),
              to: text(content.newName),
            })
          : $ts("projects.history.carriedOut", { from: text(content.previousName) });
      }
      return content.newName
        ? $ts("projects.history.cycled", { actor, to: text(content.newName) })
        : $ts("projects.history.uncycled", { actor });
    case "issue.milestone_changed":
      return content.newName
        ? $ts("projects.history.milestoned", { actor, to: text(content.newName) })
        : $ts("projects.history.unmilestoned", { actor });
    default:
      return $ts("projects.history.other", { actor, type: entry.type });
  }
}
</script>

<template>
  <ol class="space-y-2">
    <li v-for="entry in props.entries" :key="entry.id" class="flex gap-2.5 text-xs">
      <span class="mt-1.5 size-1.5 shrink-0 rounded-full bg-border-strong" aria-hidden="true" />
      <span class="min-w-0 flex-1">
        <span class="text-foreground">{{ describe(entry) }}</span>
        <time :datetime="entry.at" :title="fully(entry.at)" class="ml-1.5 text-muted-foreground">
          {{ whenBriefly(entry.at) }}
        </time>
      </span>
    </li>
  </ol>
</template>
