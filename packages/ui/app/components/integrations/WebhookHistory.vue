<script setup lang="ts">
import type { WebhookAttempt, WebhookDelivery } from "@tdc/types";
import { CircleCheck, CircleX, Clock, Hourglass } from "lucide-vue-next";

import { onDayAtTime } from "#layers/ui/app/lib/dates";

/**
 * What has been tried for one webhook, newest first.
 *
 * The question somebody opens this with is "which deliveries failed, and why",
 * so every attempt says its outcome in words beside its icon — never by colour
 * alone — with the status the receiver answered and, when it did not answer,
 * what went wrong on the way. Deliveries still waiting say when they will next
 * be tried, because a queue that is quiet looks the same as one that is stuck.
 */

const props = defineProps<{
  attempts: WebhookAttempt[];
  deliveries: WebhookDelivery[];
}>();

const { $t, $ts } = useI18n();

const waiting = computed(() =>
  props.deliveries.filter((delivery) => delivery.state === "pending" && delivery.nextAttemptAt),
);

function outcomeLabel(attempt: WebhookAttempt): string {
  switch (attempt.outcome) {
    case "delivered":
      return $ts("integrations.outcomeDelivered");
    case "rate_limited":
      return $ts("integrations.outcomeHeldBack");
    default:
      return $ts("integrations.outcomeFailed");
  }
}

/** What the receiver said, or what stopped it saying anything. */
function detail(attempt: WebhookAttempt): string {
  return attempt.error || attempt.response || "";
}
</script>

<template>
  <div class="space-y-3">
    <p v-if="waiting.length" class="flex items-center gap-1.5 text-xs text-muted-foreground">
      <Hourglass class="size-3.5 shrink-0" aria-hidden="true" />
      {{
        $t("integrations.waitingDeliveries", {
          count: waiting.length,
          when: onDayAtTime(waiting.at(-1)!.nextAttemptAt!),
        })
      }}
    </p>

    <p v-if="!props.attempts.length" class="text-xs text-muted-foreground">
      {{ $t("integrations.noAttemptsYet") }}
    </p>

    <div v-else class="overflow-x-auto rounded-md border">
      <table class="w-full text-left text-xs">
        <caption class="sr-only">
          {{
            $t("integrations.historyCaption")
          }}
        </caption>
        <thead class="bg-muted/50 text-muted-foreground">
          <tr>
            <th scope="col" class="px-2.5 py-1.5 font-medium">{{ $t("integrations.when") }}</th>
            <th scope="col" class="px-2.5 py-1.5 font-medium">{{ $t("integrations.event") }}</th>
            <th scope="col" class="px-2.5 py-1.5 font-medium">{{ $t("integrations.attempt") }}</th>
            <th scope="col" class="px-2.5 py-1.5 font-medium">
              {{ $t("integrations.outcome") }}
            </th>
            <th scope="col" class="px-2.5 py-1.5 font-medium">{{ $t("integrations.status") }}</th>
            <th scope="col" class="px-2.5 py-1.5 font-medium">{{ $t("integrations.detail") }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="attempt in props.attempts" :key="attempt.id" class="border-t align-top">
            <td class="px-2.5 py-1.5 whitespace-nowrap">
              <time :datetime="attempt.at">{{ onDayAtTime(attempt.at) }}</time>
            </td>
            <td class="px-2.5 py-1.5 font-mono">{{ attempt.kind }}</td>
            <td class="px-2.5 py-1.5 tabular-nums">{{ attempt.attempt }}</td>
            <td class="px-2.5 py-1.5">
              <!-- The colour is on the icon only. The words carry the meaning,
                   in the foreground colour every theme guarantees contrast
                   for; a pale green or amber word would not. -->
              <span class="inline-flex items-center gap-1 font-medium">
                <CircleCheck
                  v-if="attempt.outcome === 'delivered'"
                  class="size-3.5 text-success"
                  aria-hidden="true"
                />
                <Clock
                  v-else-if="attempt.outcome === 'rate_limited'"
                  class="size-3.5 text-warning"
                  aria-hidden="true"
                />
                <CircleX v-else class="size-3.5 text-destructive" aria-hidden="true" />
                {{ outcomeLabel(attempt) }}
              </span>
            </td>
            <td class="px-2.5 py-1.5 font-mono tabular-nums">{{ attempt.status ?? "—" }}</td>
            <td class="max-w-72 px-2.5 py-1.5 break-words text-muted-foreground">
              {{ detail(attempt) }}
              <span class="block text-[0.6875rem] tabular-nums">
                {{ $t("integrations.tookMilliseconds", { ms: attempt.durationMs }) }}
              </span>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>
