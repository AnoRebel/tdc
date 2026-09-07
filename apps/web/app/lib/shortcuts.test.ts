import { describe, expect, it } from "vitest";

import { isTyping } from "./shortcuts";

/**
 * Builds a keystroke as the browser would deliver it.
 */
function keystroke(options: {
  tag?: string;
  contentEditable?: boolean;
  ctrl?: boolean;
  meta?: boolean;
}): KeyboardEvent {
  const target = {
    tagName: options.tag ?? "DIV",
    isContentEditable: options.contentEditable ?? false,
  };

  return {
    target,
    ctrlKey: options.ctrl ?? false,
    metaKey: options.meta ?? false,
  } as unknown as KeyboardEvent;
}

/*
 * The rule the whole feature stands on.
 *
 * A shortcut that fires while somebody is writing a message eats the character
 * they meant to type. One stolen letter costs more trust than ten shortcuts
 * are worth, so the guard is tested before anything that depends on it.
 */
describe("knowing when somebody is typing rather than commanding", () => {
  it("treats every kind of text entry as typing", () => {
    for (const tag of ["INPUT", "TEXTAREA", "SELECT"]) {
      expect(isTyping(keystroke({ tag }))).toBe(true);
    }
    expect(isTyping(keystroke({ tag: "DIV", contentEditable: true }))).toBe(true);
  });

  it("treats the rest of the page as not typing", () => {
    for (const tag of ["DIV", "BODY", "BUTTON", "A", "MAIN"]) {
      expect(isTyping(keystroke({ tag }))).toBe(false);
    }
  });

  /*
   * A modifier settles it either way. Nobody types Ctrl-K into a sentence, so
   * a shortcut carrying one is unambiguous even inside a message box — which
   * is exactly where somebody is when they want to search.
   */
  it("lets a modified keystroke through a text box", () => {
    expect(isTyping(keystroke({ tag: "TEXTAREA", ctrl: true }))).toBe(false);
    expect(isTyping(keystroke({ tag: "INPUT", meta: true }))).toBe(false);
  });

  it("survives a keystroke with no target", () => {
    const orphan = { target: null, ctrlKey: false, metaKey: false } as unknown as KeyboardEvent;
    expect(() => isTyping(orphan)).not.toThrow();
    expect(isTyping(orphan)).toBe(false);
  });
});
