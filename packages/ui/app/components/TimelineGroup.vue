<script setup lang="ts">
import type { MessageVia } from "@tdc/types";
import { Avatar } from "#layers/ui/app/components/ui/avatar";
import { MessageGroup } from "#layers/ui/app/components/ui/message";
import { atTime } from "#layers/ui/app/lib/dates";
import type { MessageRun } from "#layers/ui/app/lib/timeline";
import type { useReactions } from "#layers/ui/app/composables/useReactions";

/*
 * A run of messages from one sender: their name and avatar once, then each
 * message. Shared by a channel and a thread, so a reply from a program is
 * labelled as one in a thread exactly as it is in the channel.
 */
defineProps<{
  run: MessageRun;
  inThread?: boolean;
  reactions: ReturnType<typeof useReactions>;
  idPrefix?: string;
}>();

const { $t } = useI18n();

/**
 * The program a run came from, when it was not a person — read from the
 * message's own content, where the server put it and nothing a sender supplies
 * can add or remove it.
 */
function viaOf(run: MessageRun): MessageVia | undefined {
  return (run.items[0]?.content as { via?: MessageVia } | undefined)?.via;
}

function shortName(address: string): string {
  return address.split("@")[0] ?? address;
}
</script>

<template>
  <MessageGroup :mine="run.mine">
    <div class="flex items-center gap-2 px-0.5" :class="run.mine ? 'flex-row-reverse' : 'flex-row'">
      <Avatar :name="viaOf(run)?.name ?? run.sender" size="sm" />
      <span class="text-xs font-semibold">
        {{ run.mine ? $t("room.you") : (viaOf(run)?.name ?? shortName(run.sender)) }}
      </span>
      <!-- A program is named as one, in words, so nobody reads a webhook's or
           a bot's message as a person's. -->
      <span
        v-if="viaOf(run)"
        class="rounded border border-border px-1 text-[0.625rem] font-medium tracking-wide text-muted-foreground uppercase"
        data-sender-kind
      >
        {{ viaOf(run)!.kind === "bot" ? $t("integrations.bot") : $t("integrations.webhook") }}
      </span>
      <time class="text-[0.6875rem] text-muted-foreground" :datetime="run.at">
        {{ atTime(run.at) }}
      </time>
    </div>

    <TimelineMessage
      v-for="(message, position) in run.items"
      :key="message.id"
      :message="message"
      :mine="run.mine"
      :sender="run.sender"
      :position="position"
      :in-thread="inThread"
      :reactions="reactions"
      :id-prefix="idPrefix"
    />
  </MessageGroup>
</template>
