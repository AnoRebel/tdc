<script setup lang="ts">
import { nameAt } from "#layers/ui/app/lib/names";
import type { ProjectHistoryEntry } from "@tdc/types";
import { fully, onCalendarDay, whenBriefly } from "#layers/ui/app/lib/dates";

const { $ts } = useI18n();

/**
 * What happened to a project itself, newest first: its details, members,
 * states, labels, priorities, cycles and milestones.
 *
 * Shown as recorded — the names things had when they changed — and with no
 * control on any entry, because the server has no way to change one.
 */
const props = defineProps<{ entries: ProjectHistoryEntry[] }>();

/** What to call the person; the server itself when the entry names nobody. */
function who(address: string): string {
  if (!address.includes("@")) return $ts("projects.history.server");
  return nameAt(address);
}

function text(value: unknown): string {
  return typeof value === "string" ? value : String(value ?? "");
}

function day(value: unknown): string {
  return value ? onCalendarDay(text(value)) : "";
}

function field(
  content: Record<string, unknown>,
  name: string,
): Record<string, unknown> | undefined {
  const value = content[name];
  return value && typeof value === "object" ? (value as Record<string, unknown>) : undefined;
}

/** One entry, said as one or more sentences: a single change can touch several things. */
function describe(entry: ProjectHistoryEntry): string[] {
  const actor = who(entry.actor);
  const content = entry.content;
  const name = text(content.name);
  const previous = field(content, "previous");

  switch (entry.type) {
    case "project.created":
      return [$ts("projects.projectHistory.created", { actor, name })];
    case "project.updated": {
      const said: string[] = [];
      const renamed = field(content, "name");
      if (renamed) {
        said.push(
          $ts("projects.projectHistory.renamed", {
            actor,
            from: text(renamed.previous),
            to: text(renamed.new),
          }),
        );
      }
      if (field(content, "description")) {
        said.push($ts("projects.projectHistory.described", { actor }));
      }
      const visibility = field(content, "visibility");
      if (visibility) {
        said.push(
          visibility.new === "private"
            ? $ts("projects.projectHistory.madePrivate", { actor })
            : $ts("projects.projectHistory.madePublic", { actor }),
        );
      }
      const zone = field(content, "timeZone");
      if (zone) said.push($ts("projects.projectHistory.zoned", { actor, to: text(zone.new) }));
      const policy = field(content, "cyclePolicy");
      if (policy) {
        said.push(
          $ts("projects.projectHistory.policy", {
            actor,
            to: $ts(`projects.planning.policies.${text(policy.new)}`),
          }),
        );
      }
      return said;
    }
    case "project.member_added": {
      const role = $ts(`projects.projectHistory.roles.${text(content.role)}`);
      const person = who(text(content.member));
      return [
        content.previous
          ? $ts("projects.projectHistory.memberRole", { actor, who: person, role })
          : $ts("projects.projectHistory.memberAdded", { actor, who: person, role }),
      ];
    }
    case "project.member_removed":
      return [
        $ts("projects.projectHistory.memberRemoved", { actor, who: who(text(content.member)) }),
      ];
    case "project.state_added":
      return [$ts("projects.projectHistory.stateAdded", { actor, name })];
    case "project.state_changed": {
      if (content.order) return [$ts("projects.projectHistory.statesReordered", { actor })];
      const said: string[] = [];
      if (previous && previous.name !== content.name) {
        said.push(
          $ts("projects.projectHistory.stateRenamed", {
            actor,
            from: text(previous.name),
            to: name,
          }),
        );
      }
      if (previous && previous.isCompletion !== content.isCompletion) {
        said.push(
          content.isCompletion
            ? $ts("projects.projectHistory.stateDone", { actor, name })
            : $ts("projects.projectHistory.stateNotDone", { actor, name }),
        );
      }
      return said;
    }
    case "project.state_removed":
      return [$ts("projects.projectHistory.stateRemoved", { actor, name })];
    case "project.label_added":
      return [$ts("projects.projectHistory.labelAdded", { actor, name })];
    case "project.label_changed":
      return [
        $ts("projects.projectHistory.labelChanged", {
          actor,
          from: text(content.previousName),
          to: name,
        }),
      ];
    case "project.label_removed":
      return [$ts("projects.projectHistory.labelRemoved", { actor, name })];
    case "project.priority_added":
      return [$ts("projects.projectHistory.priorityAdded", { actor, name })];
    case "project.priority_changed":
      return content.order
        ? [$ts("projects.projectHistory.prioritiesReordered", { actor })]
        : [
            $ts("projects.projectHistory.priorityRenamed", {
              actor,
              from: text(content.previousName),
              to: name,
            }),
          ];
    case "project.priority_removed":
      return [$ts("projects.projectHistory.priorityRemoved", { actor, name })];
    case "project.cycle_added":
      return [
        $ts("projects.projectHistory.cycleAdded", {
          actor,
          name,
          from: day(content.startsOn),
          to: day(content.endsOn),
        }),
      ];
    case "project.cycle_changed":
      return [
        $ts("projects.projectHistory.cycleChanged", {
          actor,
          name,
          from: day(content.startsOn),
          to: day(content.endsOn),
        }),
      ];
    case "project.cycle_removed":
      return [$ts("projects.projectHistory.cycleRemoved", { actor, name })];
    case "project.cycle_ended":
      return [$ts("projects.projectHistory.cycleEnded", { name })];
    case "project.milestone_added":
      return [$ts("projects.projectHistory.milestoneAdded", { actor, name })];
    case "project.milestone_changed":
      return [$ts("projects.projectHistory.milestoneChanged", { actor, name })];
    case "project.milestone_removed":
      return [$ts("projects.projectHistory.milestoneRemoved", { actor, name })];
    case "project.baseline":
      return [$ts("projects.projectHistory.baseline")];
    default:
      return [$ts("projects.history.other", { actor, type: entry.type })];
  }
}
</script>

<template>
  <ol class="space-y-2">
    <li v-for="entry in props.entries" :key="entry.id" class="flex gap-2.5 text-xs">
      <span class="mt-1.5 size-1.5 shrink-0 rounded-full bg-border-strong" aria-hidden="true" />
      <span class="min-w-0 flex-1">
        <span class="text-foreground">{{ describe(entry).join(" · ") }}</span>
        <time :datetime="entry.at" :title="fully(entry.at)" class="ml-1.5 text-muted-foreground">
          {{ whenBriefly(entry.at) }}
        </time>
      </span>
    </li>
  </ol>
</template>
