import { onKeyStroke } from "@vueuse/core";

import { isTyping } from "~/lib/shortcuts";

/**
 * One keyboard shortcut, as somebody reading the reference sees it.
 */
export interface Shortcut {
  /** The keys, in the order they are pressed. */
  keys: string[];
  /** What it does, in the words the reference shows. */
  description: string;
  /** Which part of the application it belongs to, for grouping. */
  group: "Navigation" | "Conversation" | "Everywhere";
}

/**
 * Whether this is the platform's own command modifier.
 *
 * Command on a Mac and Control elsewhere, which is what every application
 * these people already use binds. Accepting either would mean Ctrl-K doing
 * something unexpected on a Mac, where Ctrl is a different modifier.
 */
function hasCommandModifier(event: KeyboardEvent): boolean {
  const mac = navigator.platform.toUpperCase().includes("MAC");
  return mac ? event.metaKey : event.ctrlKey;
}

/**
 * The shortcuts this application answers to.
 *
 * Listed here rather than declared where each is handled, so the reference
 * shown to somebody pressing "?" is the same list that is bound — a reference
 * assembled by hand is one that drifts from what the keys actually do.
 */
export const shortcuts: Shortcut[] = [
  { keys: ["Ctrl", "K"], description: "Search messages, channels and people", group: "Everywhere" },
  { keys: ["?"], description: "Show this list", group: "Everywhere" },
  { keys: ["Ctrl", "Shift", "K"], description: "Switch channel", group: "Navigation" },
  { keys: ["Alt", "↑"], description: "Previous channel", group: "Navigation" },
  { keys: ["Alt", "↓"], description: "Next channel", group: "Navigation" },
  {
    keys: ["Esc"],
    description: "Close what is open, or leave the message box",
    group: "Everywhere",
  },
  { keys: ["C"], description: "Write a message", group: "Conversation" },
  { keys: ["T"], description: "Open the thread on the last message", group: "Conversation" },
];

/** What the application can be asked to do from the keyboard. */
export interface ShortcutActions {
  search?: () => void;
  switchChannel?: () => void;
  previousChannel?: () => void;
  nextChannel?: () => void;
  composer?: () => void;
  thread?: () => void;
  reference?: () => void;
}

/**
 * Binds the shortcuts.
 *
 * Every unmodified one checks isTyping first, so none of them reach somebody
 * writing a message. The modified ones do not need to and deliberately do not:
 * Ctrl-K should open search from inside the message box, which is where
 * somebody usually is when they want it.
 */
export function useShortcuts(actions: ShortcutActions) {
  function run(action: (() => void) | undefined, event: KeyboardEvent) {
    if (!action) return;
    event.preventDefault();
    action();
  }

  // Search: from anywhere, including the message box.
  onKeyStroke("k", (event) => {
    if (!hasCommandModifier(event) || event.shiftKey) return;
    run(actions.search, event);
  });

  // Switching channel is the same key with Shift, which is what an application
  // with both a search and a switcher conventionally does.
  onKeyStroke("K", (event) => {
    if (!hasCommandModifier(event) || !event.shiftKey) return;
    run(actions.switchChannel, event);
  });

  onKeyStroke("ArrowUp", (event) => {
    if (!event.altKey) return;
    run(actions.previousChannel, event);
  });

  onKeyStroke("ArrowDown", (event) => {
    if (!event.altKey) return;
    run(actions.nextChannel, event);
  });

  // The unmodified ones, which must never fire while somebody is writing.
  onKeyStroke("c", (event) => {
    if (isTyping(event) || event.metaKey || event.ctrlKey || event.altKey) return;
    run(actions.composer, event);
  });

  onKeyStroke("t", (event) => {
    if (isTyping(event) || event.metaKey || event.ctrlKey || event.altKey) return;
    run(actions.thread, event);
  });

  onKeyStroke("?", (event) => {
    if (isTyping(event)) return;
    run(actions.reference, event);
  });
}
