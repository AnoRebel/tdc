import { readFileSync } from "node:fs";
import { join } from "node:path";

import { describe, expect, it } from "vitest";

import {
  SEQUENCE_TIMEOUT_MS,
  SHORTCUTS,
  SHORTCUT_GROUPS,
  bindingKeys,
  createShortcutDispatcher,
  isTyping,
} from "./shortcuts";

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

/*
 * The dispatcher, fed keystrokes the way the window would deliver them.
 *
 * What it must never do matters more than what it does: fire while somebody is
 * typing, fire when somebody has switched shortcuts off, or reach past a modal
 * dialog into the screen behind it.
 */
describe("turning keystrokes into shortcuts", () => {
  interface Press {
    key: string;
    tag?: string;
    ctrl?: boolean;
    meta?: boolean;
    alt?: boolean;
    shift?: boolean;
    composing?: boolean;
    altGraph?: boolean;
  }

  function press(options: Press): KeyboardEvent {
    let prevented = false;
    return {
      key: options.key,
      target: { tagName: options.tag ?? "BODY", isContentEditable: false },
      ctrlKey: options.ctrl ?? false,
      metaKey: options.meta ?? false,
      altKey: options.alt ?? false,
      shiftKey: options.shift ?? false,
      isComposing: options.composing ?? false,
      getModifierState: (name: string) => name === "AltGraph" && (options.altGraph ?? false),
      get defaultPrevented() {
        return prevented;
      },
      preventDefault() {
        prevented = true;
      },
    } as unknown as KeyboardEvent;
  }

  function setup(options: { enabled?: boolean; modal?: boolean; mac?: boolean } = {}) {
    const ran: string[] = [];
    let clock = 0;
    const handle = createShortcutDispatcher(
      SHORTCUTS,
      {
        enabled: () => options.enabled ?? true,
        modalOpen: () => options.modal ?? false,
        mac: options.mac ?? false,
        run: (id) => {
          ran.push(id);
          return true;
        },
      },
      () => clock,
    );
    return {
      ran,
      handle,
      wait: (ms: number) => (clock += ms),
    };
  }

  it("answers a plain key outside a text field", () => {
    const { ran, handle } = setup();
    const event = press({ key: "c" });

    expect(handle(event)).toBe("composer");
    expect(ran).toEqual(["composer"]);
    expect(event.defaultPrevented).toBe(true);
  });

  it("never fires a plain key inside a text field", () => {
    const { ran, handle } = setup();
    for (const tag of ["INPUT", "TEXTAREA", "SELECT"]) {
      for (const key of ["c", "e", "t", "r", "m", "n", "?", "["]) {
        const event = press({ key, tag });
        expect(handle(event), `${key} in ${tag}`).toBeNull();
        expect(event.defaultPrevented).toBe(false);
      }
    }
    expect(ran).toEqual([]);
  });

  it("still lets Escape and the modified shortcuts through a text field", () => {
    const { ran, handle } = setup();
    handle(press({ key: "k", ctrl: true, tag: "TEXTAREA" }));
    handle(press({ key: "Escape", tag: "TEXTAREA" }));
    handle(press({ key: "ArrowDown", alt: true, tag: "TEXTAREA" }));

    expect(ran).toEqual(["search", "escape", "nextConversation"]);
  });

  /*
   * Switched off means off, except for the two that are how anybody gets in
   * and out of anything: Escape, and the search the sidebar advertises.
   */
  it("does nothing but Escape and search once shortcuts are switched off", () => {
    const { ran, handle } = setup({ enabled: false });
    for (const key of ["c", "e", "t", "r", "m", "n", "?", "["]) handle(press({ key }));
    handle(press({ key: "ArrowUp", alt: true }));
    handle(press({ key: "g" }));
    handle(press({ key: "p" }));
    expect(ran).toEqual([]);

    handle(press({ key: "Escape" }));
    handle(press({ key: "k", ctrl: true }));
    expect(ran).toEqual(["escape", "search"]);
  });

  it("leaves the screen behind a modal dialog alone", () => {
    const { ran, handle } = setup({ modal: true });
    handle(press({ key: "c" }));
    handle(press({ key: "ArrowDown", alt: true }));
    expect(ran).toEqual([]);

    // The dialog's own way out, and the list of keys, still work.
    handle(press({ key: "Escape" }));
    handle(press({ key: "?", shift: true }));
    expect(ran).toEqual(["escape", "reference"]);
  });

  it("ignores a key that something nearer the focus already handled", () => {
    const { ran, handle } = setup();
    const event = press({ key: "Escape" });
    event.preventDefault();
    handle(event);
    expect(ran).toEqual([]);
  });

  it("ignores keys that are part of composing a character", () => {
    const { ran, handle } = setup();
    handle(press({ key: "c", composing: true }));
    expect(ran).toEqual([]);
  });

  it("uses Command on a Mac and Control elsewhere", () => {
    const mac = setup({ mac: true });
    mac.handle(press({ key: "k", ctrl: true }));
    expect(mac.ran).toEqual([]);
    mac.handle(press({ key: "k", meta: true }));
    expect(mac.ran).toEqual(["search"]);

    const other = setup();
    other.handle(press({ key: "k", meta: true }));
    expect(other.ran).toEqual([]);
  });

  it("tells search and switching apart by Shift", () => {
    const { ran, handle } = setup();
    handle(press({ key: "K", ctrl: true, shift: true }));
    handle(press({ key: "k", ctrl: true }));
    expect(ran).toEqual(["switchConversation", "search"]);
  });

  it("does not mistake a capital letter for its shortcut", () => {
    const { ran, handle } = setup();
    handle(press({ key: "C", shift: true }));
    expect(ran).toEqual([]);
  });

  /*
   * On many European layouts "[" is typed with AltGr, which Windows reports as
   * Control and Alt together. Read literally, that would be a chord no
   * shortcut uses, and the key would do nothing for those people.
   */
  it("treats a key typed with AltGr as the key it produced", () => {
    const { ran, handle } = setup();
    handle(press({ key: "[", ctrl: true, alt: true, altGraph: true }));
    expect(ran).toEqual(["sidebar"]);
  });

  it("follows a sequence typed within the time allowed", () => {
    const { ran, handle, wait } = setup();
    expect(handle(press({ key: "g" }))).toBeNull();
    wait(400);
    expect(handle(press({ key: "p" }))).toBe("goProjects");
    expect(ran).toEqual(["goProjects"]);
  });

  it("forgets the first key of a sequence after a pause", () => {
    const { ran, handle, wait } = setup();
    handle(press({ key: "g" }));
    wait(SEQUENCE_TIMEOUT_MS + 1);
    handle(press({ key: "p" }));
    expect(ran).toEqual([]);
  });

  it("does not start a sequence from a key typed into a field", () => {
    const { ran, handle } = setup();
    handle(press({ key: "g", tag: "INPUT" }));
    handle(press({ key: "p" }));
    expect(ran).toEqual([]);
  });

  it("leaves a key alone when nothing on screen handles it", () => {
    const handle = createShortcutDispatcher(SHORTCUTS, {
      enabled: () => true,
      modalOpen: () => false,
      mac: false,
      run: () => false,
    });
    const event = press({ key: "t" });
    expect(handle(event)).toBeNull();
    expect(event.defaultPrevented).toBe(false);
  });
});

/*
 * The reference is generated from the same list that is bound, so what it can
 * get wrong is the words: every shortcut has to be described, in both
 * languages.
 */
describe("describing the shortcuts", () => {
  const locales = ["en", "sw"].map((code) => ({
    code,
    strings: JSON.parse(
      readFileSync(join(import.meta.dirname, "..", "..", "locales", `${code}.json`), "utf8"),
    ) as {
      interface: { shortcuts: Record<string, string>; shortcutGroups: Record<string, string> };
    },
  }));

  it("describes every shortcut and every group in every language", () => {
    for (const { code, strings } of locales) {
      for (const shortcut of SHORTCUTS) {
        expect(strings.interface.shortcuts[shortcut.id], `${code}: ${shortcut.id}`).toBeTruthy();
      }
      for (const group of SHORTCUT_GROUPS) {
        expect(strings.interface.shortcutGroups[group], `${code}: ${group}`).toBeTruthy();
      }
    }
  });

  it("gives every shortcut a unique name and at least one key", () => {
    const ids = SHORTCUTS.map((shortcut) => shortcut.id);
    expect(new Set(ids).size).toBe(ids.length);
    for (const shortcut of SHORTCUTS) expect(shortcut.bindings.length).toBeGreaterThan(0);
  });

  it("writes keys the way people read them", () => {
    expect(bindingKeys({ chord: { key: "k", command: true, shift: true } })).toEqual({
      keys: ["Ctrl", "Shift", "K"],
      sequence: false,
    });
    expect(bindingKeys({ chord: { key: "ArrowUp", alt: true } }).keys).toEqual(["Alt", "↑"]);
    expect(bindingKeys({ sequence: ["g", "p"] })).toEqual({ keys: ["G", "P"], sequence: true });
  });

  /*
   * Nothing claimed by the browser or the desktop menu. The desktop menu binds
   * the command key with N, Q, K, H, "/" and ","; K and "/" are listed here on
   * purpose, as the same actions the menu performs.
   */
  it("binds the command modifier only where the desktop menu means the same thing", () => {
    const commanded = SHORTCUTS.flatMap((shortcut) =>
      shortcut.bindings.flatMap((binding) =>
        "chord" in binding && binding.chord.command ? [binding.chord.key.toLowerCase()] : [],
      ),
    );
    expect(new Set(commanded)).toEqual(new Set(["k", "/"]));
  });
});
