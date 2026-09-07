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
