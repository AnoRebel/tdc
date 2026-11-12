import { readFileSync } from "node:fs";
import { join } from "node:path";

import { describe, expect, it } from "vitest";

/*
 * What the components taken from Inspira UI had to be corrected on.
 *
 * The originals were reviewed before adopting anything, which is where these
 * rules come from: an animated list that never stopped, and a number ticker
 * that made a count unreadable while it counted. Neither failure is visible in
 * a screenshot, which is why they are written down as checks.
 */

const HERE = import.meta.dirname;
const count = readFileSync(join(HERE, "Count.vue"), "utf8");
const arrive = readFileSync(join(HERE, "..", "arrive", "Arrive.vue"), "utf8");

describe("the count on a badge", () => {
  /*
   * The original counts up over a second. On a landing page that is a
   * flourish; on an unread badge it means the number is wrong for as long as
   * the animation runs, and somebody glancing at it reads a stale figure.
   */
  it("shows the number immediately rather than counting to it", () => {
    // Nothing that interpolates the value itself.
    expect(count).not.toContain("useTransition");
    expect(count).not.toMatch(/duration:\s*\d{3,}/);
  });

  /*
   * Every timer this starts is cleared when the component goes.
   *
   * The list component this was taken from starts a loop in onMounted and
   * never stops it — it keeps cycling after the component is unmounted, for
   * as long as the tab is open. A timer is the small version of that mistake.
   */
  it("clears its timer when it goes away", () => {
    expect(count).toContain("onUnmounted");
    expect(count).toContain("clearTimeout");
  });

  /*
   * Somebody who asked their system for less movement asked this application
   * too. An animation that ignores that is one they cannot turn off.
   */
  it("honours a preference for less motion", () => {
    expect(count).toContain("motion-reduce:");
  });
});

describe("things that arrive", () => {
  /*
   * Through a named preset from the motion stylesheet, which is keyed off the
   * interface's motion preference — entrance, exit and reordering alike, since
   * a leave animation that keeps moving is the same problem arriving later.
   * Classes of its own would be classes the preference cannot reach.
   */
  it("honours a preference for less motion", () => {
    expect(arrive).toMatch(/name="motion-[a-z]+"/);
    expect(arrive, "classes of its own would ignore the preference").not.toMatch(
      /(enter|leave|move)-[a-z-]*class=/,
    );
  });

  /*
   * No animation library. The idea is one property over 200ms, and 548 KB of
   * dependency to express that is a strange trade in an application people
   * open on a phone.
   */
  it("needs nothing beyond CSS", () => {
    expect(arrive).not.toContain("motion-v");
    expect(arrive).not.toContain("@vueuse/motion");
    expect(arrive).not.toContain("gsap");
  });
});
