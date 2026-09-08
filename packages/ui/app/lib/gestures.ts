/*
 * The decisions a gesture makes, separated from the plumbing that observes it.
 *
 * Whether a drag is a reply, a channel change or a scroll is a question about
 * two numbers and where the finger started — no touch events, no library, no
 * browser. Kept here so it can be tested directly, because the alternative is
 * synthesising touch sequences and hoping the library agrees with them.
 */

/**
 * How far a finger travels before a swipe counts.
 *
 * Sixty pixels. Short enough to feel responsive, long enough that scrolling a
 * conversation or tapping a message is not mistaken for a swipe — which is the
 * failure that makes a gesture feel like a bug rather than a feature.
 */
export const SWIPE_THRESHOLD = 60;

/**
 * How far past the top somebody pulls before the conversation refreshes.
 *
 * Further than a swipe, because the alternative is refreshing every time
 * somebody scrolls up quickly to read what they missed.
 */
export const PULL_THRESHOLD = 80;

/** What a completed drag turned out to mean. */
export type SwipeOutcome = "reply" | "previous" | "next" | "nothing";

/**
 * Decides what a drag across the conversation meant.
 *
 * One function for both gestures, because they are the same motion told apart
 * by where it began: a drag that starts on a message is about that message,
 * and one starting on the space around it is about the conversation. Deciding
 * them separately is how the first version replied and changed channel at
 * once.
 */
export function readSwipe(input: {
  lengthX: number;
  lengthY: number;
  startedOnMessage: boolean;
}): SwipeOutcome {
  const { lengthX, lengthY, startedOnMessage } = input;

  /*
   * A mostly-vertical drag is somebody scrolling.
   *
   * Reading a long conversation on a phone means dragging up and down, and a
   * scroll that drifted sideways should stay a scroll — without this, reading
   * changes channel by accident.
   */
  if (Math.abs(lengthY) > Math.abs(lengthX)) return "nothing";
  if (Math.abs(lengthX) < SWIPE_THRESHOLD) return "nothing";

  // A negative length is a leftward drag: the content moves aside, which is
  // how a reply gesture reads and how "forward" reads for a channel.
  if (startedOnMessage) return lengthX < 0 ? "reply" : "nothing";

  return lengthX < 0 ? "next" : "previous";
}

/**
 * How far a pull has travelled, given where the conversation is scrolled.
 *
 * Zero unless already at the very top: pulling in the middle of a conversation
 * is scrolling, and treating it as a refresh would make scrolling unreliable.
 */
export function readPull(input: { lengthY: number; scrollTop: number }): number {
  if (input.scrollTop > 0 || input.lengthY > 0) return 0;
  return Math.min(-input.lengthY, PULL_THRESHOLD * 1.5);
}

/** Whether a pull went far enough to mean it. */
export function pullTriggersRefresh(distance: number): boolean {
  return distance >= PULL_THRESHOLD;
}
