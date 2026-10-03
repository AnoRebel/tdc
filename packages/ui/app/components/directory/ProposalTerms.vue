<script setup lang="ts">
import type { ProposalVersion } from "@tdc/types";
import { formatMoney } from "#layers/ui/app/lib/money";

const { $t } = useI18n();

/**
 * One version's terms, as both people see them.
 *
 * Given the version before it, each part that differs is marked, so a
 * revision answering a change request shows where it answered — the reader
 * does not have to compare two lists by eye.
 */

const props = defineProps<{ version: ProposalVersion; previous?: ProposalVersion }>();

const changed = computed(() => {
  const before = props.previous;
  if (!before) return { scope: false, timeline: false, currency: false, items: [] as boolean[] };
  return {
    scope: before.scope !== props.version.scope,
    timeline: before.timeline !== props.version.timeline,
    currency: before.currency !== props.version.currency,
    items: props.version.lineItems.map((item, index) => {
      const old = before.lineItems[index];
      return !old || old.description !== item.description || old.amountMinor !== item.amountMinor;
    }),
  };
});
</script>

<template>
  <dl class="space-y-2 text-sm" :data-proposal-version="props.version.version">
    <div>
      <dt class="text-xs text-muted-foreground">
        {{ $t("directory.agreement.parts.scope") }}
        <span v-if="changed.scope" class="ml-1 text-primary" data-changed>
          {{ $t("directory.agreement.changed") }}
        </span>
      </dt>
      <dd class="whitespace-pre-line">{{ props.version.scope }}</dd>
    </div>
    <div>
      <dt class="text-xs text-muted-foreground">
        {{ $t("directory.agreement.parts.timeline") }}
        <span v-if="changed.timeline" class="ml-1 text-primary" data-changed>
          {{ $t("directory.agreement.changed") }}
        </span>
      </dt>
      <dd>{{ props.version.timeline }}</dd>
    </div>
    <div>
      <dt class="text-xs text-muted-foreground">
        {{ $t("directory.agreement.items") }}
        <span v-if="changed.currency" class="ml-1 text-primary" data-changed>
          {{ $t("directory.agreement.currencyChanged") }}
        </span>
      </dt>
      <dd>
        <ol class="divide-y rounded-md border">
          <li
            v-for="(item, index) in props.version.lineItems"
            :key="index"
            class="flex items-baseline justify-between gap-3 px-2.5 py-1.5"
            data-line
          >
            <span class="min-w-0">
              <span class="text-muted-foreground">{{ index + 1 }}.</span>
              {{ item.description }}
              <span v-if="changed.items[index]" class="ml-1 text-xs text-primary" data-changed>
                {{ $t("directory.agreement.changed") }}
              </span>
            </span>
            <span class="shrink-0 tabular-nums">{{
              formatMoney(item.amountMinor, props.version.currency)
            }}</span>
          </li>
          <li class="flex items-baseline justify-between gap-3 px-2.5 py-1.5 font-semibold">
            <span>{{ $t("directory.agreement.total") }}</span>
            <span class="tabular-nums" data-total>{{
              formatMoney(props.version.totalMinor, props.version.currency)
            }}</span>
          </li>
        </ol>
      </dd>
    </div>
  </dl>
</template>
