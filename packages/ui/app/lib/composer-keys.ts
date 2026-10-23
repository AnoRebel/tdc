import { nextTick, type Ref } from "vue";

import { applyFormat, insideOpenFence, type Format } from "./formatting";
import { isMac } from "./platform";
import { SHORTCUTS, matchesChord } from "./shortcuts";

/*
 * The keys a message box answers to, wherever one is: the composer of a
 * channel, the composer of a thread, and the box a message is edited in.
 *
 * One copy, so that a shortcut learned in one of them works in all of them.
 * The message box stays a plain text box: formatting is the Markdown people
 * type, and these only save typing it.
 */

const FORMAT_KEYS: [string, Format][] = [
  ["formatBold", "bold"],
  ["formatItalic", "italic"],
  ["formatStrikethrough", "strikethrough"],
  ["formatCode", "code"],
  ["formatCodeBlock", "codeBlock"],
];

/** Which format a keystroke asks for, if any. */
export function formatFor(event: KeyboardEvent): Format | null {
  const mac = isMac();
  for (const [id, format] of FORMAT_KEYS) {
    const shortcut = SHORTCUTS.find((candidate) => candidate.id === id);
    const pressed = shortcut?.bindings.some(
      (binding) => "chord" in binding && matchesChord(binding.chord, event, mac),
    );
    if (pressed) return format;
  }
  return null;
}

/**
 * Wraps the selection in a text box and keeps what was wrapped selected — or
 * takes the markers off again if it is already wrapped.
 */
export function formatField(
  element: HTMLTextAreaElement | null,
  model: Ref<string>,
  format: Format,
  after?: () => void,
) {
  if (!element) return;
  const next = applyFormat(
    { text: model.value, start: element.selectionStart, end: element.selectionEnd },
    format,
  );
  model.value = next.text;
  void nextTick(() => {
    element.focus();
    element.setSelectionRange(next.start, next.end);
    after?.();
  });
}

/**
 * The formatting shortcuts, and Enter.
 *
 * Enter sends and Shift-Enter starts a new line — except inside a code block
 * that has been opened and not yet closed, where Enter is a new line too.
 * Somebody typing code wants lines, and half a code block is never what they
 * meant to send; typing the closing fence makes Enter send again. Nothing
 * happens while an input method is composing a character, whose Enter is
 * confirming the character rather than sending.
 */
export function messageKeys(
  event: KeyboardEvent,
  element: HTMLTextAreaElement | null,
  model: Ref<string>,
  submit: () => void,
  after?: () => void,
) {
  if (event.isComposing) return;

  const format = formatFor(event);
  if (format) {
    event.preventDefault();
    formatField(element, model, format, after);
    return;
  }

  if (event.key !== "Enter" || event.shiftKey || event.altKey || event.ctrlKey || event.metaKey) {
    return;
  }
  const cursor = element?.selectionStart ?? model.value.length;
  if (insideOpenFence(model.value, cursor)) return;

  event.preventDefault();
  submit();
}
