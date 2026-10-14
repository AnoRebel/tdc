<script setup lang="ts">
import type { ProposalVersion } from "@tdc/types";
import { formatMoney } from "#layers/ui/app/lib/money";

const { $t, $ts } = useI18n();

/**
 * What one set of terms changes from another, and nothing else.
 *
 * Each changed part is shown as it was and as it would be, side by side in
 * one row, so "the price went from 1,500,000 to 1,800,000" is read directly
 * rather than worked out by comparing two lists. Unchanged parts are left out;
 * the full terms are a click away in the timeline. Deliverables are compared
 * by their position, the way the proposal numbers them.
 *
 * Before and after are marked with <del> and <ins>, and each carries a word
 * for a screen reader as well, since strike-through alone says nothing aloud.
 */

const props = defineProps<{ before: ProposalVersion; after: ProposalVersion }>();

interface Change {
  key: string;
  label: string;
  before?: string;
  after?: string;
  /** A price, kept on one line of its own so a narrow screen never splits it. */
  beforeAmount?: string;
  afterAmount?: string;
}

const changes = computed<Change[]>(() => {
  const { before, after } = props;
  const list: Change[] = [];
  if (before.scope !== after.scope)
    list.push({
      key: "scope",
      label: $ts("directory.agreement.parts.scope"),
      before: before.scope,
      after: after.scope,
    });
  if (before.timeline !== after.timeline)
    list.push({
      key: "timeline",
      label: $ts("directory.agreement.parts.timeline"),
      before: before.timeline,
      after: after.timeline,
    });
  if (before.currency !== after.currency)
    list.push({
      key: "currency",
      label: $ts("directory.agreement.parts.currency"),
      before: before.currency,
      after: after.currency,
    });
  const count = Math.max(before.lineItems.length, after.lineItems.length);
  for (let index = 0; index < count; index++) {
    const old = before.lineItems[index];
    const now = after.lineItems[index];
    const price = (item: typeof old, currency: string) =>
      item ? formatMoney(item.amountMinor, currency) : undefined;
    if (
      old?.description !== now?.description ||
      price(old, before.currency) !== price(now, after.currency)
    )
      list.push({
        key: `item:${index + 1}`,
        label: $ts("directory.agreement.parts.item", { number: index + 1 }),
        before: old?.description,
        after: now?.description,
        beforeAmount: price(old, before.currency),
        afterAmount: price(now, after.currency),
      });
  }
  if (before.totalMinor !== after.totalMinor || before.currency !== after.currency)
    list.push({
      key: "total",
      label: $ts("directory.agreement.total"),
      beforeAmount: formatMoney(before.totalMinor, before.currency),
      afterAmount: formatMoney(after.totalMinor, after.currency),
    });
  return list;
});
</script>

<template>
  <div data-terms-diff>
    <p v-if="!changes.length" class="text-sm text-muted-foreground">
      {{ $t("directory.revision.noChanges") }}
    </p>
    <table v-else class="w-full table-fixed border-collapse text-sm">
      <caption class="sr-only">
        {{
          $t("directory.revision.diffCaption", {
            from: props.before.version,
            to: props.after.version,
          })
        }}
      </caption>
      <thead class="text-left text-xs text-muted-foreground">
        <tr>
          <th scope="col" class="w-20 py-1 pr-2 font-normal sm:w-28">
            {{ $t("directory.revision.part") }}
          </th>
          <th scope="col" class="py-1 pr-2 font-normal">
            {{ $t("directory.revision.was", { number: props.before.version }) }}
          </th>
          <th scope="col" class="py-1 font-normal">
            {{ $t("directory.revision.becomes", { number: props.after.version }) }}
          </th>
        </tr>
      </thead>
      <tbody>
        <tr
          v-for="change in changes"
          :key="change.key"
          class="border-t align-top"
          :data-change="change.key"
        >
          <th scope="row" class="py-1.5 pr-2 text-left text-xs font-medium">{{ change.label }}</th>
          <td class="py-1.5 pr-2 break-words whitespace-pre-line">
            <del
              v-if="change.before || change.beforeAmount"
              class="inline-block rounded bg-destructive/10 px-1 text-destructive decoration-destructive/60"
            >
              <span class="sr-only">{{ $t("directory.revision.removed") }}</span
              >{{ change.before
              }}<span v-if="change.beforeAmount" class="block tabular-nums whitespace-nowrap">{{
                change.beforeAmount
              }}</span>
            </del>
            <span v-else class="text-xs text-muted-foreground">{{
              $t("directory.revision.absent")
            }}</span>
          </td>
          <td class="py-1.5 break-words whitespace-pre-line">
            <ins
              v-if="change.after || change.afterAmount"
              class="inline-block rounded bg-primary/10 px-1 text-foreground no-underline"
            >
              <span class="sr-only">{{ $t("directory.revision.added") }}</span
              >{{ change.after
              }}<span v-if="change.afterAmount" class="block tabular-nums whitespace-nowrap">{{
                change.afterAmount
              }}</span>
            </ins>
            <span v-else class="text-xs text-muted-foreground">{{
              $t("directory.revision.dropped")
            }}</span>
          </td>
        </tr>
      </tbody>
    </table>
  </div>
</template>
