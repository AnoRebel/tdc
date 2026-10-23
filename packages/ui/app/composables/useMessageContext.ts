import type { IssueFromMessage, PrivateReply, ThreadSummary } from "@tdc/types";
import { inject, provide, type ComputedRef, type InjectionKey, type Ref } from "vue";

import type { MenuItem } from "#layers/ui/app/components/ContextMenu.vue";
import type { ShownMessage } from "#layers/ui/app/lib/timeline";
import type { useContextMenu } from "#layers/ui/app/composables/useContextMenu";

/*
 * What a message can have done to it, in the conversation it is shown in.
 *
 * Provided once by the conversation and read by every message in it — the
 * channel's and its open thread's alike. A reply is a message: editing it,
 * deleting it, reporting it, pinning it, filing an issue from it and pressing
 * its buttons are the same operations with the same rules, and a second copy
 * of them in the thread would be the copy that falls behind.
 */
export interface MessageContext {
  roomId: Ref<string>;
  /** Whether the reader is in the conversation, and so may act in it. */
  member: ComputedRef<boolean>;
  /** The reader's own address, for their own reactions. */
  selfAddress: ComputedRef<string>;

  /** The menu for one message, holding only what this reader may do. */
  menuFor(message: ShownMessage, mine: boolean, sender: string, inThread: boolean): MenuItem[];
  contextMenu: ReturnType<typeof useContextMenu>;

  openThread(messageId: string): void;
  /** Reply counts and unread replies, for a message in the channel. */
  threadSummary(messageId: string): ThreadSummary | undefined;

  pinnedIds: Ref<Set<string>> | ComputedRef<Set<string>>;
  issuesFor(messageId: string): IssueFromMessage[];

  /** The message being edited, and the text it is being edited to. */
  editing: Ref<string | null>;
  editDraft: Ref<string>;
  startEditing(message: { id: string; body: string }): void;
  saveEdit(messageId: string): Promise<void>;
  confirmDelete(message: { id: string; body: string }): void;

  pressAction(message: ShownMessage, name: string, value?: string): Promise<void>;
  /** What the last press on each message came to, for the live region. */
  pressOutcome: Ref<Record<string, string>>;
  /** Programs' answers meant for this reader alone, by the message they answer. */
  privateReplies: Ref<Record<string, PrivateReply[]>>;
  dismissPrivate(messageId: string, replyId: string): void;

  selecting: ComputedRef<boolean>;
  selected: Ref<Set<string>>;
  toggleSelected(messageId: string): void;
}

const messageContextKey = Symbol("tdc.messages") as InjectionKey<MessageContext>;

export function provideMessageContext(context: MessageContext) {
  provide(messageContextKey, context);
}

/**
 * The conversation's operations on messages.
 *
 * Loud when missing: a message rendered outside a conversation has nothing to
 * edit, react or reply through, and failing on the first click would be a
 * long way from the cause.
 */
export function useMessageContext(): MessageContext {
  const context = inject(messageContextKey, null);
  if (!context) {
    throw new Error("A message was rendered outside a conversation that provides its actions.");
  }
  return context;
}
