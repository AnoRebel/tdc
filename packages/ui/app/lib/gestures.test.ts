import { describe, expect, it } from "vitest";

import {
  PULL_THRESHOLD,
  SWIPE_THRESHOLD,
  pullTriggersRefresh,
  readPull,
  readSwipe,
} from "./gestures";

/** A leftward drag of the given distance, mostly horizontal. */
function drag(lengthX: number, options: { lengthY?: number; onMessage?: boolean } = {}) {
  return readSwipe({
    lengthX,
    lengthY: options.lengthY ?? 0,
    startedOnMessage: options.onMessage ?? false,
  });
}

describe("reading a swipe across the conversation", () => {
  /*
   * The bug the first version shipped: both gestures listened on the same
   * element, so swiping a message replied to it and changed channel at once.
   * Where the drag began is what tells them apart.
   */
  it("never does two things at once", () => {
    expect(drag(-150, { onMessage: true })).toBe("reply");
    expect(drag(-150, { onMessage: false })).toBe("next");
  });

  it("moves between channels only off a message", () => {
    expect(drag(-150)).toBe("next");
    expect(drag(150)).toBe("previous");
  });

  /*
   * Rightward on a message does nothing rather than replying. A message
   * answering to both directions would reply on the swipe somebody made for
   * something else, and the two motions are close enough to be confused.
   */
  it("replies in one direction only", () => {
    expect(drag(-150, { onMessage: true })).toBe("reply");
    expect(drag(150, { onMessage: true })).toBe("nothing");
  });

  /*
   * Reading a long conversation on a phone means dragging up and down. A
   * scroll that drifted sideways has to stay a scroll, or reading changes
   * channel by accident.
   */
  it("leaves a scroll alone", () => {
    expect(drag(-100, { lengthY: -300 })).toBe("nothing");
    expect(drag(-100, { lengthY: -300, onMessage: true })).toBe("nothing");
  });

  it("ignores a drag too short to have been meant", () => {
    expect(drag(-(SWIPE_THRESHOLD - 1))).toBe("nothing");
    expect(drag(-(SWIPE_THRESHOLD - 1), { onMessage: true })).toBe("nothing");
  });

  it("acts once the drag is long enough", () => {
    expect(drag(-(SWIPE_THRESHOLD + 1))).toBe("next");
  });

  // A tap is a drag of nothing, and must not be a gesture.
  it("treats a tap as nothing", () => {
    expect(drag(0, { onMessage: true })).toBe("nothing");
    expect(drag(0)).toBe("nothing");
  });
});

describe("reading a pull", () => {
  /*
   * Only from the very top. Pulling in the middle of a conversation is
   * scrolling, and treating it as a refresh makes scrolling unreliable.
   */
  it("only counts at the top", () => {
    expect(readPull({ lengthY: -100, scrollTop: 0 })).toBeGreaterThan(0);
    expect(readPull({ lengthY: -100, scrollTop: 1 })).toBe(0);
    expect(readPull({ lengthY: -100, scrollTop: 400 })).toBe(0);
  });

  it("only counts downwards", () => {
    // A positive length is a drag upwards, which is scrolling on.
    expect(readPull({ lengthY: 100, scrollTop: 0 })).toBe(0);
  });

  /*
   * Bounded, so the indicator cannot be dragged off the screen by somebody
   * pulling as far as their arm goes.
   */
  it("stops growing past a limit", () => {
    expect(readPull({ lengthY: -10000, scrollTop: 0 })).toBeLessThanOrEqual(PULL_THRESHOLD * 1.5);
  });

  it("refreshes only once pulled far enough", () => {
    expect(pullTriggersRefresh(PULL_THRESHOLD - 1)).toBe(false);
    expect(pullTriggersRefresh(PULL_THRESHOLD)).toBe(true);
  });

  /*
   * Further than a swipe. Otherwise every quick scroll up to read what was
   * missed would refresh the conversation.
   */
  it("asks for more than a swipe does", () => {
    expect(PULL_THRESHOLD).toBeGreaterThan(SWIPE_THRESHOLD);
  });
});
