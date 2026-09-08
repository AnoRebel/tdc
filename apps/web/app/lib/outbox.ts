/**
 * Messages composed while the server could not be reached.
 *
 * The rule this exists for: somebody typing on a bus should not lose what they
 * wrote, and should not be told it was sent when it was not. Both halves
 * matter — a message that silently vanishes and a message that silently
 * pretends are the same betrayal in opposite directions.
 */

import { readFailure, type Failure } from "./reachability";

// Re-exported so that callers thinking of this as an outbox concern find it
// here, while the rule itself lives where three other modules can reach it.
export { readFailure, type Failure };

/** A message waiting to be sent. */
export interface Queued {
  /** Doubles as the idempotency token, so a retry cannot post twice. */
  clientToken: string;
  roomId: string;
  body: string;
  attachmentIds: string[];
  /** When it was composed, so the timeline can order it against what arrived. */
  composedAt: number;
  /** How many times sending has been tried and failed. */
  attempts: number;
}

/*
 * maxAttempts bounds how long something stays queued.
 *
 * Not for the server's sake: a retry is idempotent and costs one request. It
 * is for the person, who deserves to be told a message is not going to send
 * rather than watching it sit there indefinitely looking like it might.
 */
export const maxAttempts = 5;

/**
 * Whether a queued message should be tried again.
 *
 * A refusal never is. An unreachable one is, until it has been tried enough
 * times that continuing would be pretending.
 */
export function shouldRetry(message: Queued, failure: Failure): boolean {
  if (failure === "refused") return false;
  return message.attempts < maxAttempts;
}

/**
 * The queue after a message failed to send.
 *
 * Returns a new array rather than mutating, so a caller holding the old one
 * for a render is not changed underneath it.
 */
export function afterFailure(queue: Queued[], clientToken: string, failure: Failure): Queued[] {
  const result: Queued[] = [];

  for (const message of queue) {
    if (message.clientToken !== clientToken) {
      result.push(message);
      continue;
    }

    const attempted = { ...message, attempts: message.attempts + 1 };
    if (shouldRetry(attempted, failure)) result.push(attempted);
  }

  return result;
}

/**
 * The queue after a message was sent.
 *
 * Removed by token rather than by position: the queue may have been added to
 * while the request was in flight, and removing an index would drop whatever
 * happened to be sitting there.
 */
export function afterSending(queue: Queued[], clientToken: string): Queued[] {
  return queue.filter((message) => message.clientToken !== clientToken);
}

/**
 * The order in which a queue is sent.
 *
 * Oldest first, so a conversation arrives in the order it was written. Sending
 * newest first would reorder somebody's own sentences, which reads as
 * incoherence rather than as a delay.
 */
export function sendOrder(queue: Queued[]): Queued[] {
  return [...queue].sort((a, b) => a.composedAt - b.composedAt);
}
