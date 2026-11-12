import { readFileSync } from "node:fs";
import { join } from "node:path";

import { describe, expect, it } from "vitest";

/*
 * The buttons a message offers, and what makes them usable by everybody.
 *
 * Read rather than mounted, matching the form components' tests: these are
 * rules about what the source guarantees, and a check needing a browser would
 * not run on every save. The Playwright suite exercises them in a real one,
 * where the questions are about focus order and what axe finds.
 */

const HERE = import.meta.dirname;
const source = readFileSync(join(HERE, "MessageActions.vue"), "utf8");

describe("the buttons a message offers", () => {
  /*
   * A div with a click handler is reachable by a pointer and by nothing else.
   * A button is tab-reachable, activates on Enter and Space, and is announced
   * as a button — none of which anybody has to implement.
   */
  it("uses real buttons", () => {
    expect(source, "the row should render Button, not a clickable element").toContain("<Button");
    expect(source).toContain('type="button"');
  });

  /*
   * A press whose outcome is only shown is a press that succeeded silently
   * for anybody not watching that row.
   */
  it("announces how a press ended", () => {
    expect(source, "the outcome needs a live region").toContain('aria-live="polite"');
    expect(source, "and a role that names it as status").toContain('role="status"');
  });

  /*
   * Polite rather than assertive. A button press is not worth interrupting
   * whatever somebody was being read; assertive is for things that are.
   */
  it("does not interrupt to say so", () => {
    expect(source).not.toContain('aria-live="assertive"');
  });

  /*
   * An icon inside a labelled button is decoration. Left readable it is
   * announced as an extra, unnamed thing between the reader and the label.
   */
  it("hides its icons from the reader", () => {
    const icons = source.match(/<(ExternalLink|Spinner)[^>]*>/g) ?? [];
    const decorative = icons.filter((tag) => tag.includes("aria-hidden"));
    expect(decorative.length, "every icon in a labelled button should be aria-hidden").toBe(
      icons.filter((tag) => tag.includes("ExternalLink")).length,
    );
  });

  /*
   * Why a button cannot be pressed, said rather than implied.
   *
   * A disabled button announces "unavailable" and nothing else, which leaves
   * somebody guessing whether it is them, the button, or a bug.
   */
  it("says why nothing is pressable", () => {
    expect(source).toContain("room.joinToAnswer");
  });

  /*
   * An expired button is disabled and relabelled rather than removed. A
   * control that vanishes leaves somebody wondering whether they imagined it,
   * and a reader who had tabbed to it loses their place.
   */
  it("keeps an expired button visible and says it is over", () => {
    expect(source).toContain("room.actionNoLongerOffered");
    expect(source).toContain("expired(action)");
  });

  /*
   * Every string a reader meets goes through the translation layer. A button
   * row that hard-coded "Join this channel to answer" would be one more
   * sentence only English speakers can read.
   */
  it("says nothing only English speakers can read", () => {
    const template = source.slice(source.indexOf("<template>"));
    const literals = [...template.matchAll(/>\s*([A-Z][A-Za-z ,.'’—-]{6,60})\s*</g)]
      .map((match) => match[1]!.trim())
      .filter((text) => !text.includes("{{"));
    expect(literals, "these are written in English in the template").toEqual([]);
  });

  /*
   * The label comes from the message, and the message comes from whoever
   * posted it. Rendering it as markup would let a program put anything into
   * every reader's client.
   */
  it("never renders a label as markup", () => {
    expect(source, "a label is text").not.toContain("v-html");
  });
});
