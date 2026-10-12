/*
 * The parts of keyboard handling that are decisions rather than bindings.
 *
 * Here rather than in the composable because they are pure — a keystroke in,
 * an answer out — and a test should be able to ask them directly without
 * mounting anything.
 */

/**
 * Whether a keystroke is somebody typing rather than commanding.
 *
 * This is the rule the whole feature stands on. A shortcut that fires while
 * somebody is writing a message eats the character they meant to type, and
 * they lose trust in the keyboard entirely — one stolen letter is worse than
 * ten shortcuts are worth.
 *
 * A modifier changes that: nobody types Ctrl-K into a sentence, so a shortcut
 * carrying one is unambiguous even in a text box.
 */
export function isTyping(event: KeyboardEvent): boolean {
  if (event.metaKey || event.ctrlKey) return false;

  const target = event.target as HTMLElement | null;
  if (!target) return false;

  const tag = target.tagName;
  return tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT" || target.isContentEditable;
}

/** Where a shortcut belongs, which is how the reference groups them. */
export const SHORTCUT_GROUPS = [
  "everywhere",
  "navigation",
  "conversation",
  "formatting",
  "projects",
] as const;
export type ShortcutGroup = (typeof SHORTCUT_GROUPS)[number];

/**
 * One key pressed with its modifiers.
 *
 * `command` is the platform's command modifier — Command on a Mac and Control
 * elsewhere — which is what every application these people already use binds.
 * Accepting either would mean Ctrl-K doing something unexpected on a Mac, where
 * Ctrl is a different modifier.
 */
export interface Chord {
  key: string;
  command?: boolean;
  alt?: boolean;
  shift?: boolean;
}

/** One way of pressing a shortcut: a chord, or plain keys one after another. */
export type Binding = { chord: Chord } | { sequence: string[] };

/** One keyboard shortcut, as the reference shows it and the keyboard answers it. */
export interface ShortcutDefinition {
  id: string;
  group: ShortcutGroup;
  bindings: Binding[];
  /**
   * Whether it works from inside a text field.
   *
   * Only for keys that cannot be typed into a sentence — a modifier chord, or
   * Escape — so a shortcut marked this way never steals a character.
   */
  inTextFields?: boolean;
  /**
   * Whether it keeps working when somebody has switched shortcuts off.
   *
   * Escape and search: one is how anybody gets out of anything, and the other
   * is the way in that the interface itself advertises in the sidebar.
   */
  alwaysOn?: boolean;
  /**
   * Whether a component handles it rather than the application.
   *
   * Listed so the reference is complete — the arrow key in an empty message
   * box, the keys on a board card's handle — but bound where the focus is,
   * because only that component knows what the key means there.
   */
  local?: boolean;
}

/**
 * Every shortcut, in the order the reference lists them.
 *
 * One list for both jobs, so the reference shown to somebody pressing "?" is
 * the list that is bound — a reference assembled by hand is one that drifts
 * from what the keys actually do.
 *
 * Chosen to stay clear of what the browser and the operating system already
 * claim. Ctrl and Command are only ever used with K (which the desktop menu
 * also carries) and with "/" (likewise) — apart from the message box's own
 * formatting keys, further down; everything else is a plain key, an
 * Alt-arrow, or a "g then…" sequence, none of which a browser binds. In
 * particular nothing here touches Ctrl-N, -T, -W, -L, -H, -Q, -comma or
 * Alt-left/right, which are a new window, a tab, closing it, the address bar,
 * history or hiding the desktop window, quitting, settings, and
 * back and forward.
 */
export const SHORTCUTS: ShortcutDefinition[] = [
  {
    id: "search",
    group: "everywhere",
    bindings: [{ chord: { key: "k", command: true } }],
    inTextFields: true,
    alwaysOn: true,
  },
  {
    id: "reference",
    group: "everywhere",
    bindings: [{ chord: { key: "?" } }, { chord: { key: "/", command: true } }],
  },
  {
    id: "escape",
    group: "everywhere",
    bindings: [{ chord: { key: "Escape" } }],
    inTextFields: true,
    alwaysOn: true,
  },
  { id: "sidebar", group: "everywhere", bindings: [{ chord: { key: "[" } }] },

  {
    id: "switchConversation",
    group: "navigation",
    bindings: [{ chord: { key: "k", command: true, shift: true } }],
    inTextFields: true,
  },
  {
    id: "previousConversation",
    group: "navigation",
    bindings: [{ chord: { key: "ArrowUp", alt: true } }],
    inTextFields: true,
  },
  {
    id: "nextConversation",
    group: "navigation",
    bindings: [{ chord: { key: "ArrowDown", alt: true } }],
    inTextFields: true,
  },
  {
    id: "previousUnread",
    group: "navigation",
    bindings: [{ chord: { key: "ArrowUp", alt: true, shift: true } }],
    inTextFields: true,
  },
  {
    id: "nextUnread",
    group: "navigation",
    bindings: [{ chord: { key: "ArrowDown", alt: true, shift: true } }],
    inTextFields: true,
  },
  { id: "goConversations", group: "navigation", bindings: [{ sequence: ["g", "h"] }] },
  { id: "goProjects", group: "navigation", bindings: [{ sequence: ["g", "p"] }] },
  { id: "goIssues", group: "navigation", bindings: [{ sequence: ["g", "i"] }] },
  { id: "goBoard", group: "navigation", bindings: [{ sequence: ["g", "b"] }] },
  { id: "goDirectory", group: "navigation", bindings: [{ sequence: ["g", "d"] }] },
  { id: "goSettings", group: "navigation", bindings: [{ sequence: ["g", "s"] }] },

  { id: "composer", group: "conversation", bindings: [{ chord: { key: "c" } }] },
  { id: "editLast", group: "conversation", bindings: [{ chord: { key: "e" } }] },
  {
    id: "editLastFromComposer",
    group: "conversation",
    bindings: [{ chord: { key: "ArrowUp" } }],
    local: true,
  },
  { id: "thread", group: "conversation", bindings: [{ chord: { key: "t" } }] },
  { id: "react", group: "conversation", bindings: [{ chord: { key: "r" } }] },
  { id: "markRead", group: "conversation", bindings: [{ chord: { key: "m" } }] },

  /*
   * Formatting, in the message box only, around whatever is selected.
   *
   * Ctrl-B and Ctrl-I are what every editor binds, and the browser's own
   * meanings for them — bookmarks, page information — give way to a page
   * that handles them while somebody is typing. They are the exception to the
   * rule above, and a deliberate one: bound only inside the message box, where
   * the browser's meaning is never what somebody writing wanted.
   */
  {
    id: "formatBold",
    group: "formatting",
    bindings: [{ chord: { key: "b", command: true } }],
    local: true,
  },
  {
    id: "formatItalic",
    group: "formatting",
    bindings: [{ chord: { key: "i", command: true } }],
    local: true,
  },
  {
    id: "formatStrikethrough",
    group: "formatting",
    bindings: [{ chord: { key: "x", command: true, shift: true } }],
    local: true,
  },
  {
    id: "formatCode",
    group: "formatting",
    bindings: [{ chord: { key: "e", command: true } }],
    local: true,
  },
  {
    id: "formatCodeBlock",
    group: "formatting",
    bindings: [{ chord: { key: "c", command: true, shift: true } }],
    local: true,
  },
  {
    id: "newLineInMessage",
    group: "formatting",
    bindings: [{ chord: { key: "Enter", shift: true } }],
    local: true,
  },

  { id: "newIssue", group: "projects", bindings: [{ chord: { key: "n" } }] },
  {
    id: "pickUpCard",
    group: "projects",
    bindings: [{ chord: { key: "Enter" } }, { chord: { key: " " } }],
    local: true,
  },
  {
    id: "moveCard",
    group: "projects",
    bindings: [
      { chord: { key: "ArrowLeft", shift: true } },
      { chord: { key: "ArrowRight", shift: true } },
    ],
    local: true,
  },
];

/** The identifiers, for anything that registers an action against one. */
export type ShortcutId = (typeof SHORTCUTS)[number]["id"];

/** Whether a key is a single letter, whose case is decided by Shift. */
function isLetter(key: string): boolean {
  return key.length === 1 && key.toLowerCase() !== key.toUpperCase();
}

/**
 * Whether a keystroke is this chord.
 *
 * AltGr is not Alt: on many European layouts it is how "[" and "/" are typed
 * at all, and browsers on Windows report it as Ctrl and Alt together. A key
 * produced with it is treated as the plain key it produced.
 */
export function matchesChord(chord: Chord, event: KeyboardEvent, mac: boolean): boolean {
  const altGraph = event.getModifierState?.("AltGraph") ?? false;
  const command = mac ? event.metaKey : event.ctrlKey && !altGraph;
  const otherCommand = mac ? event.ctrlKey : event.metaKey;
  const alt = event.altKey && !altGraph;

  if (Boolean(chord.command) !== command || otherCommand) return false;
  if (Boolean(chord.alt) !== alt) return false;

  if (isLetter(chord.key)) {
    if (Boolean(chord.shift) !== event.shiftKey) return false;
    return event.key.toLowerCase() === chord.key;
  }

  // A character that needs Shift to type — "?" — carries it in the key
  // itself, so Shift is only checked where the chord says something about it.
  if (chord.key.length > 1 || chord.shift !== undefined) {
    if (Boolean(chord.shift) !== event.shiftKey) return false;
  }
  return event.key === chord.key;
}

/** What the dispatcher asks of the application around it. */
export interface DispatchContext {
  /** Whether somebody has shortcuts switched on. */
  enabled: () => boolean;
  /** Whether a modal dialog is open, which owns the keyboard while it is. */
  modalOpen: () => boolean;
  /** Whether this is a Mac, where the command modifier is Command. */
  mac: boolean;
  /** Runs a shortcut, and says whether anything was done. */
  run: (id: ShortcutId, event: KeyboardEvent) => boolean;
}

/**
 * Whether a shortcut may answer this keystroke at all.
 *
 * Separate from matching, so the rules about *when* are tested on their own:
 * never while composing text in an input method, never while typing unless the
 * shortcut is one that cannot be typed, never when switched off unless it is
 * one of the two that always work, and never behind a modal dialog, which owns
 * the keyboard until it closes.
 */
export function mayAnswer(
  shortcut: ShortcutDefinition,
  event: KeyboardEvent,
  context: Pick<DispatchContext, "enabled" | "modalOpen">,
): boolean {
  if (shortcut.local) return false;
  // An input method mid-composition: the key is part of a character, not a
  // command, whatever it looks like.
  if (event.isComposing) return false;
  if (!shortcut.alwaysOn && !context.enabled()) return false;
  if (!shortcut.inTextFields && isTyping(event)) return false;
  if (!shortcut.alwaysOn && shortcut.id !== "reference" && context.modalOpen()) return false;
  return true;
}

/** How long the second key of a sequence is waited for. */
export const SEQUENCE_TIMEOUT_MS = 1500;

/**
 * Turns keystrokes into shortcuts.
 *
 * A function of the keystroke and the context, with the only state being the
 * first key of a sequence that is waiting for its second — so it can be tested
 * by feeding it events, without a page.
 */
export function createShortcutDispatcher(
  shortcuts: ShortcutDefinition[],
  context: DispatchContext,
  now: () => number = () => Date.now(),
) {
  let pending: { key: string; at: number } | null = null;

  const firstKeys = new Set(
    shortcuts.flatMap((shortcut) =>
      shortcut.bindings.flatMap((binding) => ("sequence" in binding ? [binding.sequence[0]!] : [])),
    ),
  );

  function attempt(shortcut: ShortcutDefinition, event: KeyboardEvent): boolean {
    if (!mayAnswer(shortcut, event, context)) return false;
    if (!context.run(shortcut.id as ShortcutId, event)) return false;
    event.preventDefault();
    return true;
  }

  return function handle(event: KeyboardEvent): ShortcutId | null {
    // Something nearer the focus has already dealt with it — a menu closing on
    // Escape, a list moving its selection — and this must not act twice.
    if (event.defaultPrevented) return null;

    const plain = !event.ctrlKey && !event.metaKey && !event.altKey && event.key.length === 1;

    // The second key of a sequence, if one is waiting.
    if (pending && plain) {
      const first = pending.key;
      const fresh = now() - pending.at <= SEQUENCE_TIMEOUT_MS;
      pending = null;

      if (fresh) {
        for (const shortcut of shortcuts) {
          const completes = shortcut.bindings.some(
            (binding) =>
              "sequence" in binding &&
              binding.sequence[0] === first &&
              binding.sequence[1] === event.key,
          );
          if (completes && attempt(shortcut, event)) return shortcut.id as ShortcutId;
        }
        return null;
      }
    }

    for (const shortcut of shortcuts) {
      const matches = shortcut.bindings.some(
        (binding) => "chord" in binding && matchesChord(binding.chord, event, context.mac),
      );
      if (matches && attempt(shortcut, event)) return shortcut.id as ShortcutId;
    }

    /*
     * The start of a sequence. Only remembered when the sequence could fire —
     * otherwise somebody typing "g" into a field and then pressing "p"
     * elsewhere a second later would find themselves on another page.
     */
    if (plain && firstKeys.has(event.key)) {
      const probe = shortcuts.find((shortcut) =>
        shortcut.bindings.some(
          (binding) => "sequence" in binding && binding.sequence[0] === event.key,
        ),
      );
      if (probe && mayAnswer(probe, event, context)) pending = { key: event.key, at: now() };
    }

    return null;
  };
}

/**
 * The keys of a binding, as a person reads them.
 *
 * "Ctrl" stands for the command modifier and is relabelled per platform where
 * it is shown, so a Mac reads ⌘.
 */
export function bindingKeys(binding: Binding): { keys: string[]; sequence: boolean } {
  const names: Record<string, string> = {
    ArrowUp: "↑",
    ArrowDown: "↓",
    ArrowLeft: "←",
    ArrowRight: "→",
    Escape: "Esc",
    Enter: "Enter",
    " ": "Space",
  };
  const label = (key: string) => names[key] ?? (isLetter(key) ? key.toUpperCase() : key);

  if ("sequence" in binding) return { keys: binding.sequence.map(label), sequence: true };

  const { chord } = binding;
  const keys: string[] = [];
  if (chord.command) keys.push("Ctrl");
  if (chord.alt) keys.push("Alt");
  if (chord.shift) keys.push("Shift");
  keys.push(label(chord.key));
  return { keys, sequence: false };
}
