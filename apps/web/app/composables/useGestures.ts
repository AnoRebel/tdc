import { useSwipe } from "@vueuse/core";

import {
  PULL_THRESHOLD,
  pullTriggersRefresh,
  readPull,
  readSwipe,
} from "#layers/ui/app/lib/gestures";

/**
 * Touch gestures over a conversation.
 *
 * `useSwipe` from VueUse observes the touches; what a drag *meant* is decided
 * in lib/gestures, which is plain arithmetic and tested directly. Splitting
 * them that way is deliberate: the observation is the library's problem, and
 * the decision is the one that goes wrong.
 *
 * `@vueuse/gesture` is not installed. It has had no release since February
 * 2024, and the library already in the tree does this job — a dependency worth
 * thirty lines is not worth carrying.
 *
 * Every gesture has a control that does the same thing. A gesture is a
 * shortcut for somebody who knows it, never the only way to reach something:
 * an interaction nobody can discover is one most people never find.
 */

/**
 * Swiping over the conversation: sideways on a message to reply, sideways on
 * the space around it to change channel.
 *
 * One listener rather than one per message. A ref and a handler per bubble
 * would mean hundreds of listeners in a long conversation, all to answer a
 * question the event already carries — and two separate listeners is how the
 * first version managed to reply and change channel with one finger.
 */
export function useConversationSwipes(
  element: Ref<HTMLElement | null>,
  actions: {
    reply: (messageId: string) => void;
    previous: () => void;
    next: () => void;
  },
) {
  /*
   * Where the gesture began, captured at the start.
   *
   * By the time it ends the finger has moved and may be over a different
   * message, or none.
   */
  const startedOn = ref<string | null>(null);
  const startedOnMessage = ref(false);

  const { lengthX, lengthY, isSwiping } = useSwipe(element, {
    // Passive, so this does not fight the browser's own scrolling: the gesture
    // is horizontal and scrolling is vertical, so they do not compete.
    passive: true,
    onSwipeStart(event) {
      const target = event.target as HTMLElement | null;
      const message = target?.closest<HTMLElement>("[data-message-id]");

      startedOn.value = message?.dataset.messageId ?? null;
      startedOnMessage.value = !!message;
    },
    onSwipeEnd() {
      const messageId = startedOn.value;
      const onMessage = startedOnMessage.value;
      startedOn.value = null;
      startedOnMessage.value = false;

      switch (
        readSwipe({
          lengthX: lengthX.value,
          lengthY: lengthY.value,
          startedOnMessage: onMessage,
        })
      ) {
        case "reply":
          if (messageId) actions.reply(messageId);
          break;
        case "next":
          actions.next();
          break;
        case "previous":
          actions.previous();
          break;
      }
    },
  });

  return { isSwiping, lengthX };
}

/**
 * Pull down at the top of a conversation to reload it.
 */
export function usePullToRefresh(
  element: Ref<HTMLElement | null>,
  onRefresh: () => void | Promise<void>,
) {
  const distance = ref(0);
  const refreshing = ref(false);

  const { lengthY, isSwiping } = useSwipe(element, {
    passive: true,
    onSwipe() {
      distance.value = readPull({
        lengthY: lengthY.value,
        scrollTop: element.value?.scrollTop ?? 0,
      });
    },
    async onSwipeEnd() {
      const pulled = distance.value;
      distance.value = 0;

      if (!pullTriggersRefresh(pulled) || refreshing.value) return;

      refreshing.value = true;
      try {
        await onRefresh();
      } finally {
        refreshing.value = false;
      }
    },
  });

  return { distance, refreshing, isSwiping, threshold: PULL_THRESHOLD };
}
