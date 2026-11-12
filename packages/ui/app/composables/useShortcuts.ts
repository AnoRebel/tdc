import { useEventListener } from "@vueuse/core";
import { onBeforeUnmount, onMounted } from "vue";

import { useInterfacePreferences } from "#layers/ui/app/composables/useInterfacePreferences";
import { isMac } from "#layers/ui/app/lib/platform";
import { SHORTCUTS, createShortcutDispatcher, type ShortcutId } from "#layers/ui/app/lib/shortcuts";

/**
 * Something a shortcut does.
 *
 * Returns false to say "not here" — a conversation's Escape with no thread
 * open, say — so the next handler gets the key instead.
 */
export type ShortcutAction = (event: KeyboardEvent) => boolean | void;

/*
 * What the screens currently showing have registered, newest last.
 *
 * Module state rather than provide/inject: the layout owns the keyboard, and
 * the conversation, the board or a panel several components down are what
 * know what a key means right now. Registration happens on mount, which is
 * client-only, so nothing here is shared between server requests.
 */
const registered = new Map<ShortcutId, ShortcutAction[]>();

/**
 * Registers what a shortcut does while the calling component is mounted.
 *
 * The most recently mounted wins, so a thread panel's Escape is tried before
 * the layout's, and a page that is not a conversation simply registers
 * nothing — the key then does nothing, which is the right outcome.
 */
export function useShortcutAction(id: ShortcutId, action: ShortcutAction) {
  onMounted(() => {
    registered.set(id, [...(registered.get(id) ?? []), action]);
  });
  onBeforeUnmount(() => {
    registered.set(
      id,
      (registered.get(id) ?? []).filter((existing) => existing !== action),
    );
  });
}

/** Runs the handlers for a shortcut, newest first, until one acts. */
function runRegistered(
  id: ShortcutId,
  event: KeyboardEvent,
  fallback: ShortcutAction | undefined,
): boolean {
  const handlers = [...(registered.get(id) ?? [])].reverse();
  if (fallback) handlers.push(fallback);

  for (const handler of handlers) {
    if (handler(event) !== false) return true;
  }
  return false;
}

/** Whether a modal dialog is open, which owns the keyboard until it closes. */
function modalOpen(): boolean {
  return typeof document !== "undefined" && Boolean(document.querySelector('[aria-modal="true"]'));
}

/**
 * Binds the shortcuts, once, for the whole application.
 *
 * The application-wide actions are passed here; what belongs to a particular
 * screen is registered by that screen with useShortcutAction. One listener
 * on the window, after everything nearer the focus has had its chance — a key
 * a menu or a list has already handled is left alone.
 */
export function useShortcuts(actions: Partial<Record<ShortcutId, ShortcutAction>>) {
  const preferences = useInterfacePreferences();

  const handle = createShortcutDispatcher(SHORTCUTS, {
    enabled: () => preferences.shortcuts.value,
    modalOpen,
    mac: isMac(),
    run: (id, event) => runRegistered(id, event, actions[id]),
  });

  useEventListener(typeof window === "undefined" ? undefined : window, "keydown", handle);
}

/** Clears every registration. For tests, which share the module between cases. */
export function forgetShortcutActions() {
  registered.clear();
}
