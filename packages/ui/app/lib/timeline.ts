import type { Attachment, ControlsUpdate, Message, MessageAction } from "@tdc/types";
import { learnName } from "./names";

/*
 * What a conversation shows, worked out from the events that make it up.
 *
 * Shared by a channel and a thread, which are the same thing at two sizes: a
 * list of messages whose edits, deletions and replaced controls arrive as
 * separate events. Folding them once here is what keeps the two from drifting
 * — a thread that forgot how an edit is applied would show the original
 * forever.
 */

/** A message as it is shown: its latest text, its controls, and what happened to it. */
export type ShownMessage = Message & {
  attachments: Attachment[];
  body: string;
  edited: boolean;
  removed: boolean;
  actions: MessageAction[];
  /** Which version of the controls is on screen, sent with a press. */
  controlsVersion: number;
};

/**
 * Folds edits, deletions and controls updates into the messages they refer to.
 *
 * Each is a separate event rather than a change to the original, so the
 * original keeps its place and its identity and only what is shown changes.
 */
export function foldMessages(events: Message[]): ShownMessage[] {
  const byId = new Map<string, Message>();
  const replaced = new Map<string, string>();
  const redacted = new Set<string>();
  // A program's latest replacement of a message's text and controls. The
  // highest version wins rather than the last to arrive, so a replay out of
  // order cannot put an older question back on screen.
  const updated = new Map<string, ControlsUpdate>();

  for (const message of events) {
    // Every message says what its sender is called, which is how a reply's
    // quote or a reaction elsewhere comes to call them the same.
    learnName(message.sender, message.senderName);
    if (message.type === "message.edited" && message.content.replaces) {
      replaced.set(message.content.replaces, message.content.body ?? "");
      continue;
    }
    if (message.type === "message.updated") {
      const update = message.content as unknown as ControlsUpdate;
      const previous = updated.get(update.updates);
      if (update.updates && (!previous || previous.version < update.version)) {
        updated.set(update.updates, update);
      }
      continue;
    }
    if (message.type === "message.deleted" && message.content.redacts) {
      redacted.add(message.content.redacts);
      continue;
    }
    byId.set(message.id, message);
  }

  return [...byId.values()]
    .filter((message) => message.type === "message.sent")
    .sort((a, b) => a.sequence - b.sequence)
    .map((message) => {
      const update = updated.get(message.id);
      const removed = redacted.has(message.id) || message.redacted;
      return {
        ...message,
        attachments: message.attachments ?? [],
        body: update?.body ?? replaced.get(message.id) ?? message.content.body ?? "",
        edited: replaced.has(message.id),
        removed,
        /*
         * Dropped from a removed message: a question whose text is gone is not
         * a question anybody can answer, and leaving the controls would invite
         * somebody to answer something they can no longer read.
         */
        actions: removed ? [] : (update?.actions ?? message.content.actions ?? []),
        controlsVersion: update?.version ?? 0,
      };
    });
}

/** A run of messages from one sender, shown as one turn. */
export interface MessageRun {
  sender: string;
  mine: boolean;
  at: string;
  items: ShownMessage[];
}

/** What kind of program, if any, a message came from. */
function viaKind(message: ShownMessage): string | undefined {
  return (message.content as { via?: { kind?: string } } | undefined)?.via?.kind;
}

/** How far apart two messages from one person can be and still read as one turn. */
const RUN_GAP = 5 * 60 * 1000;

/**
 * Groups consecutive messages from one sender, so a run reads as one turn.
 *
 * A gap of more than five minutes starts a new group even from the same
 * person: two messages an hour apart are two thoughts, not one.
 */
export function groupMessages(
  messages: ShownMessage[],
  isMine: (sender: string) => boolean,
): MessageRun[] {
  const runs: MessageRun[] = [];

  for (const message of messages) {
    const previous = runs.at(-1);
    const soonAfter =
      previous &&
      new Date(message.sentAt).getTime() - new Date(previous.items.at(-1)!.sentAt).getTime() <
        RUN_GAP;

    // An automatic reply is its own turn even beside the person's own
    // messages, so its mark is never read as covering what they typed.
    const sameKind = previous && viaKind(previous.items.at(-1)!) === viaKind(message);
    if (previous && previous.sender === message.sender && soonAfter && sameKind) {
      previous.items.push(message);
      continue;
    }

    runs.push({
      sender: message.sender,
      mine: isMine(message.sender),
      at: message.sentAt,
      items: [message],
    });
  }

  return runs;
}

/**
 * The events a conversation shows as a line of their own between messages: a
 * call starting and ending, and a recording of it starting and stopping.
 */
const LINE_TYPES = new Set([
  "call.started",
  "call.ended",
  "call.recording_started",
  "call.recording_stopped",
]);

/** One of those lines, in its place in the conversation. */
export interface TimelineLine {
  line: Message;
  at: string;
}

/**
 * Messages in runs, with the call lines between them where they happened: a
 * line breaks a run, so a turn on either side of a call reads as two.
 */
export function groupWithLines(
  messages: ShownMessage[],
  events: Message[],
  isMine: (sender: string) => boolean,
): (MessageRun | TimelineLine)[] {
  const lines = events
    .filter((event) => LINE_TYPES.has(event.type) && !event.threadRoot)
    .sort((a, b) => a.sequence - b.sequence);
  const entries: (MessageRun | TimelineLine)[] = [];
  let from = 0;
  for (const line of lines) {
    const before: ShownMessage[] = [];
    while (from < messages.length && messages[from]!.sequence < line.sequence) {
      before.push(messages[from]!);
      from += 1;
    }
    entries.push(...groupMessages(before, isMine), { line, at: line.sentAt });
  }
  entries.push(...groupMessages(messages.slice(from), isMine));
  return entries;
}

/** The name being typed after an @, and where the @ is. */
export interface MentionQuery {
  /** What follows the @ so far, possibly empty. */
  query: string;
  /** The index of the @ itself. */
  start: number;
}

/*
 * The characters a username may contain, as the server's mention rule reads
 * them (server/internal/notify). Suggesting a name the server would not
 * recognise as a mention would be offering a notification that never comes.
 */
const MENTION_BEFORE_CARET = /(?:^|\s)@([a-zA-Z0-9._-]{0,64})$/;

/**
 * Whether the caret sits just after "@name", and if so which name.
 *
 * Only at the start of a word, so an email address being typed does not open
 * a list of people. Not inside code, where "@Component" is somebody quoting
 * code rather than addressing a person.
 */
export function mentionAt(text: string, caret: number): MentionQuery | null {
  const before = text.slice(0, caret);
  const match = MENTION_BEFORE_CARET.exec(before);
  if (!match) return null;

  // An odd number of backticks before the caret means it is inside code.
  const ticks = before.split("`").length - 1;
  if (ticks % 2 === 1) return null;

  const query = match[1] ?? "";
  return { query, start: caret - query.length - 1 };
}

/** Puts a chosen name in place of what was typed, with a space after it. */
export function completeMention(
  text: string,
  mention: MentionQuery,
  caret: number,
  username: string,
): { text: string; caret: number } {
  const inserted = `@${username} `;
  // A space already following is used rather than doubled.
  const rest = text.slice(caret).replace(/^ /, "");
  return {
    text: text.slice(0, mention.start) + inserted + rest,
    caret: mention.start + inserted.length,
  };
}
