import { describe, expect, it } from "vitest";

import {
  afterReaching,
  afterSeeing,
  nothingSeen,
  readProgress,
  resumeAt,
  shouldOffer,
} from "./tours";

describe("whether a tour is offered", () => {
  it("offers one nobody has seen", () => {
    expect(shouldOffer(nothingSeen(), "chat")).toBe(true);
  });

  /*
   * The requirement 8.9 names, and the one people actually notice.
   *
   * A tour shown twice is an obstacle. Shown repeatedly it teaches somebody
   * that the application does not remember them.
   */
  it("does not offer one that was finished", () => {
    const progress = afterSeeing(nothingSeen(), "chat");

    expect(shouldOffer(progress, "chat")).toBe(false);
  });

  /*
   * Skipping is an answer, not an absence of one.
   *
   * Somebody who skipped a tour has said what they think of it more clearly
   * than somebody who sat through it, and offering it again would be ignoring
   * the clearer of the two.
   */
  it("does not offer one that was skipped", () => {
    // Skipping and finishing reach the same record deliberately.
    const skipped = afterSeeing(nothingSeen(), "chat");

    expect(shouldOffer(skipped, "chat")).toBe(false);
  });

  /*
   * Being partway through is not having seen it. An interrupted tour is one
   * somebody has not finished, and finishing it is the whole point.
   */
  it("still offers one that was only started", () => {
    const started = afterReaching(nothingSeen(), "chat", 3);

    expect(shouldOffer(started, "chat")).toBe(true);
  });
});

describe("where a tour resumes", () => {
  it("starts at the beginning when nothing was seen", () => {
    expect(resumeAt(nothingSeen(), "chat")).toBe(0);
  });

  /*
   * Somebody interrupted at step four has read the first three. Beginning
   * again teaches them nothing and costs the same attention twice.
   */
  it("resumes where it was left", () => {
    const progress = afterReaching(nothingSeen(), "chat", 4);

    expect(resumeAt(progress, "chat")).toBe(4);
  });

  /*
   * Stepping back to reread something must not lose the furthest point, or an
   * interruption there resumes earlier than it should — which reads as the
   * application forgetting.
   */
  it("does not move backwards when somebody steps back", () => {
    let progress = afterReaching(nothingSeen(), "chat", 5);
    progress = afterReaching(progress, "chat", 2);

    expect(resumeAt(progress, "chat")).toBe(5);
  });
});

describe("recording progress", () => {
  it("does not alter what it was given", () => {
    const progress = nothingSeen();
    afterSeeing(progress, "chat");
    afterReaching(progress, "chat", 3);

    expect(progress.seen).toHaveLength(0);
    expect(progress.reached.chat).toBeUndefined();
  });

  it("records a tour as seen only once", () => {
    let progress = afterSeeing(nothingSeen(), "chat");
    progress = afterSeeing(progress, "chat");

    expect(progress.seen).toEqual(["chat"]);
  });
});

describe("reading what was stored", () => {
  it("reads back what was written", () => {
    const progress = afterReaching(afterSeeing(nothingSeen(), "chat"), "chat", 2);

    expect(readProgress(JSON.stringify(progress))).toEqual(progress);
  });

  it("treats nothing stored as nothing seen", () => {
    expect(readProgress(null)).toEqual(nothingSeen());
    expect(readProgress("")).toEqual(nothingSeen());
  });

  /*
   * A tour is a convenience, and nothing here is worth failing a page load
   * over. The cost of unreadable state is one tour offered again.
   */
  it("treats unreadable state as nothing seen", () => {
    expect(readProgress("{not json")).toEqual(nothingSeen());
    expect(readProgress("null")).toEqual(nothingSeen());
    expect(readProgress('"a string"')).toEqual(nothingSeen());
    expect(readProgress("[1,2,3]").seen).toEqual([]);
  });

  /*
   * A name written by an older version, or by something else entirely, is not
   * a tour this application knows how to give. Keeping it would mean carrying
   * a value nothing can act on.
   */
  it("discards names it does not recognise", () => {
    expect(readProgress('{"seen":["chat","something-else"],"reached":{}}').seen).toEqual(["chat"]);
  });
});
