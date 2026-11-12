<script setup lang="ts">
import type { MessageLifetime } from "@tdc/types";
import {
  EyeOff,
  Languages,
  MessageSquareText,
  MoreVertical,
  Pencil,
  Pin,
  Timer,
  Trash2,
  X,
} from "lucide-vue-next";
import { Bubble } from "#layers/ui/app/components/ui/bubble";
import { Button } from "#layers/ui/app/components/ui/button";
import { Kbd } from "#layers/ui/app/components/ui/kbd";
import { Message as MessageRow } from "#layers/ui/app/components/ui/message";
import IssueLinks from "#layers/ui/app/components/projects/IssueLinks.vue";
import TranslationBody from "#layers/ui/app/components/translation/TranslationBody.vue";
import { messageKeys } from "#layers/ui/app/lib/composer-keys";
import type { ShownMessage } from "#layers/ui/app/lib/timeline";
import type { useReactions } from "#layers/ui/app/composables/useReactions";

/*
 * One message, as a channel or a thread shows it.
 *
 * The same component in both, so a reply offers everything a message does —
 * its menu, editing in place, reactions, files, a link's preview, a program's
 * buttons — and so the two cannot come to look or behave differently.
 */
const props = defineProps<{
  message: ShownMessage;
  mine: boolean;
  /** Who sent the run this message is part of. */
  sender: string;
  /** Its place in that run; anything after the first continues the bubble. */
  position: number;
  /** Shown in a thread, where replying in a thread is where the reader already is. */
  inThread?: boolean;
  reactions: ReturnType<typeof useReactions>;
  /** Prefixed to the element's id, so the same message in two places has two ids. */
  idPrefix?: string;
}>();

const { $t, $ts } = useI18n();
const context = useMessageContext();
const { shows } = useInterfacePreferences();
// Translation offered inline, with whichever provider this person chose.
const translation = useTranslation();
const translated = computed(() =>
  translation.showsTranslation(props.message.id, context.roomId.value, props.mine),
);

const editField = ref<HTMLTextAreaElement | null>(null);

const menu = () =>
  context.menuFor(props.message, props.mine, props.sender, Boolean(props.inThread));
const summary = computed(() =>
  props.inThread ? undefined : context.threadSummary(props.message.id),
);

function openMenu(event: MouseEvent) {
  const box = (event.currentTarget as HTMLElement).getBoundingClientRect();
  context.contextMenu.show(box.left, box.bottom, menu());
}

const editKeys = (event: KeyboardEvent) =>
  messageKeys(
    event,
    editField.value,
    context.editDraft,
    () => void context.saveEdit(props.message.id),
  );

function react(emoji: string) {
  void props.reactions.toggle(props.message.id, emoji, context.selfAddress.value);
}

/**
 * How long a message has left, in words.
 *
 * A seen-based lifetime has no expiry until people have read it, so it says
 * what it is waiting for rather than counting down from nothing.
 */
function lifetimeLabel(lifetime: MessageLifetime): string {
  if (!lifetime.expiresAt) {
    return lifetime.kind === "once_seen" ? $ts("room.goesWhenRead") : $ts("room.goesAfterReading");
  }

  const remaining = new Date(lifetime.expiresAt).getTime() - Date.now();
  if (remaining <= 0) return $ts("room.going");

  const minutes = Math.round(remaining / 60000);
  if (minutes < 60) return $ts("room.minutesLeft", { count: minutes });

  const hours = Math.round(minutes / 60);
  if (hours < 48) return $ts("room.hoursLeft", { count: hours });

  return $ts("room.daysLeft", { count: Math.round(hours / 24) });
}

/** The whole rule, for somebody who wants to know exactly what happens. */
function lifetimeExplanation(lifetime: MessageLifetime): string {
  switch (lifetime.kind) {
    case "once_seen":
      return $ts("room.goesOnceRead");
    case "after_seeing":
      return $ts("room.goesAfterEachRead");
    default:
      return $ts("room.goesAfterSent");
  }
}
</script>

<template>
  <MessageRow
    :id="`${idPrefix ?? ''}message-${message.id}`"
    :mine="mine"
    :data-message-id="message.id"
    v-bind="context.contextMenu.handlers(menu)"
  >
    <!-- Actions sit outside the bubble, on the side away from the edge, and
         appear only on the row being pointed at. -->
    <div
      v-if="!message.removed && context.editing.value !== message.id"
      class="flex shrink-0 items-center gap-0.5 opacity-0 transition-opacity focus-within:opacity-100 group-hover/row:opacity-100 max-md:opacity-100"
      :class="mine ? 'order-first' : 'order-last'"
    >
      <!-- A touch screen has no hover, so the controls stay visible there and
           this opens the same menu a long press would. Without it, editing and
           deleting would be unreachable on a phone. -->
      <Button
        variant="ghost"
        size="icon"
        class="md:hidden"
        :label="$ts('room.messageActions')"
        @click.stop="openMenu"
      >
        <MoreVertical class="size-4" />
      </Button>
      <Button
        v-if="context.member.value && !inThread"
        variant="ghost"
        size="icon"
        :label="$ts('room.replyInThread')"
        @click="context.openThread(message.id)"
      >
        <MessageSquareText class="size-3.5" />
      </Button>
      <!-- Somebody else's words, in the reader's language or back. -->
      <Button
        v-if="!mine && !message.removed && translation.available.value"
        variant="ghost"
        size="icon"
        :label="translated ? $ts('translation.showOriginal') : $ts('translation.translate')"
        :aria-pressed="translated"
        @click="translation.toggle(context.roomId.value, message.id, message.body, mine)"
      >
        <Languages class="size-3.5" />
      </Button>
      <Button
        v-if="mine"
        variant="ghost"
        size="icon"
        :label="$ts('action.edit')"
        @click="context.startEditing(message)"
      >
        <Pencil class="size-3.5" />
      </Button>
      <Button
        v-if="mine"
        variant="danger"
        size="icon"
        :label="$ts('action.delete')"
        @click="context.confirmDelete(message)"
      >
        <Trash2 class="size-3.5" />
      </Button>
    </div>

    <!-- Only while selecting: a checkbox beside every message all the time
         would add a column of controls to a conversation that is mostly read
         rather than acted on. -->
    <label
      v-if="context.selecting.value && !message.removed && !inThread"
      class="flex shrink-0 cursor-pointer items-center"
      :class="mine ? 'order-last' : 'order-first'"
    >
      <input
        type="checkbox"
        class="size-4 cursor-pointer accent-primary"
        :checked="context.selected.value.has(message.id)"
        :aria-label="$ts('room.selectThisMessage')"
        @change="context.toggleSelected(message.id)"
      />
    </label>

    <div
      class="flex w-fit max-w-full min-w-0 flex-col gap-1"
      :class="mine ? 'items-end' : 'items-start'"
    >
      <span
        v-if="context.pinnedIds.value.has(message.id)"
        class="flex items-center gap-1 text-[0.6875rem] text-muted-foreground"
      >
        <Pin class="size-3" />
        {{ $t("room.pinned") }}
      </span>
      <Bubble
        v-if="message.body || message.removed"
        :variant="message.removed ? 'redacted' : mine ? 'mine' : 'default'"
        :align="mine ? 'end' : 'start'"
        :continued="position > 0"
      >
        <form
          v-if="context.editing.value === message.id"
          @submit.prevent="context.saveEdit(message.id)"
        >
          <!-- Named, because there is no visible label to point at — the box
               replaces the message in place. Several lines, because a message
               may have them. Enter saves, Shift-Enter starts a line, as in the
               composer. -->
          <textarea
            ref="editField"
            v-model="context.editDraft.value"
            class="block w-full min-w-56 resize-y rounded-md border border-current/30 bg-transparent px-2 py-1 text-sm outline-none focus-visible:ring-2 focus-visible:ring-current/40"
            :rows="Math.min(10, context.editDraft.value.split('\n').length)"
            :aria-label="$ts('room.editThisMessage')"
            autofocus
            @keydown.esc="context.editing.value = null"
            @keydown="editKeys"
          />
          <!-- Not dimmed: this sits inside the sender's own bubble, a saturated
               ground, where dimmed text fell under 4.5:1. -->
          <p class="mt-1 text-[0.6875rem]">
            <Kbd class="border-current/30 bg-transparent text-current">Enter</Kbd>
            {{ $t("room.toSave") }}
            <Kbd class="border-current/30 bg-transparent text-current">Esc</Kbd>
            {{ $t("room.toCancel") }}
          </p>
        </form>

        <template v-else>
          <template v-if="message.removed">{{ $t("room.messageRemoved") }}</template>
          <TranslationBody
            v-else
            :body="message.body"
            :message-id="message.id"
            :room-id="context.roomId.value"
            :mine="mine"
          />
          <!-- The buttons this message offers. Real buttons, so a keyboard
               reaches them; the outcome is announced rather than only shown. -->
          <MessageActions
            v-if="!message.removed && message.actions.length"
            :key="`${message.id}-${message.controlsVersion}`"
            :actions="message.actions"
            :message-id="message.id"
            :can-press="context.member.value"
            :outcome="context.pressOutcome.value[message.id] ?? ''"
            @press="(name: string, value?: string) => context.pressAction(message, name, value)"
          />
          <!-- A program's answer to this reader alone, said in words, because
               "only you can see this" is what makes it different. -->
          <div
            v-for="reply in context.privateReplies.value[message.id] ?? []"
            :key="reply.id"
            class="mt-2 flex items-start gap-2 rounded-md border border-dashed border-border bg-muted/40 px-2.5 py-1.5 text-sm"
            role="note"
            :aria-label="$ts('integrations.onlyYouCanSee')"
          >
            <EyeOff class="mt-0.5 size-3.5 shrink-0" aria-hidden="true" />
            <div class="min-w-0 flex-1">
              <p class="text-[0.6875rem] font-medium">
                {{ $t("integrations.onlyYouCanSee") }}
              </p>
              <MessageBody :body="reply.body" />
            </div>
            <Button
              variant="ghost"
              size="icon"
              class="size-6"
              :label="$ts('integrations.dismiss')"
              @click="context.dismissPrivate(message.id, reply.id)"
            >
              <X class="size-3.5" aria-hidden="true" />
            </Button>
          </div>
          <IssueLinks v-if="!message.removed" :issues="context.issuesFor(message.id)" />
          <!-- At 85% rather than lower: at 60% this fell under 4.5:1 against a
               sender's own bubble. -->
          <span
            v-if="message.edited && !message.removed"
            class="ml-1 align-baseline text-[0.6875rem] opacity-85"
          >
            {{ $t("room.edited") }}
          </span>
          <!-- Said on the message itself rather than on hover: somebody deciding
               whether to answer needs to know it is on a timer. -->
          <span
            v-if="message.lifetime && !message.removed"
            class="ml-1 inline-flex items-center gap-0.5 align-baseline text-[0.6875rem] opacity-85"
            :title="lifetimeExplanation(message.lifetime)"
          >
            <Timer class="size-2.5" />
            {{ lifetimeLabel(message.lifetime) }}
          </span>
        </template>
      </Bubble>

      <AttachmentList
        v-if="!message.removed && message.attachments.length"
        :attachments="message.attachments"
      />

      <ReactionBar
        v-if="!message.removed"
        :reactions="reactions.forEvent(message.id)"
        :can-react="context.member.value"
        @react="react"
      />

      <!-- Only the first link: a message full of them would otherwise produce
           a wall of cards taller than the conversation. -->
      <LinkPreview
        v-if="!message.removed && shows('previews') && firstLinkIn(message.body)"
        :key="firstLinkIn(message.body) ?? ''"
        :url="firstLinkIn(message.body)!"
      />

      <!-- The thread under this message, right beneath it, with what is new
           in it marked — a count alone does not say whether to look. -->
      <button
        v-if="summary?.replyCount"
        type="button"
        class="flex items-center gap-1.5 rounded-md px-1.5 py-0.5 text-xs font-medium text-primary transition hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
        @click="context.openThread(message.id)"
      >
        <MessageSquareText class="size-3" aria-hidden="true" />
        {{
          summary.replyCount === 1
            ? $t("room.oneReply")
            : $t("room.manyReplies", { count: summary.replyCount })
        }}
        <span
          v-if="summary.unread"
          class="rounded-full bg-primary px-1.5 text-[0.625rem] leading-4 text-primary-foreground tabular"
        >
          {{ $t("room.newReplies", { count: summary.unread }) }}
        </span>
      </button>
    </div>
  </MessageRow>
</template>
