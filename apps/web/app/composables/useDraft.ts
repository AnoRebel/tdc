/*
 * storageKey holds unsent text, per conversation.
 *
 * Drafts are kept apart from the outbox on purpose: a queued message is
 * something somebody finished and meant to send, and a draft is something they
 * are still writing. Losing the first is a broken promise; losing the second
 * is merely rude, and they deserve different treatment on failure.
 */
const storageKey = "tdc.drafts";

/**
 * Text somebody has typed and not yet sent.
 *
 * Kept across reloads so that an update, a crash, or a closed tab does not
 * throw away a half-written message. The application asks people to reload for
 * a new version, and asking that of somebody mid-sentence is only reasonable
 * if the sentence survives it.
 */
export function useDrafts() {
  function all(): Record<string, string> {
    if (import.meta.server) return {};

    try {
      const stored = localStorage.getItem(storageKey);
      if (!stored) return {};

      const parsed: unknown = JSON.parse(stored);
      return typeof parsed === "object" && parsed !== null
        ? (parsed as Record<string, string>)
        : {};
    } catch {
      return {};
    }
  }

  /** What was being written in one conversation. */
  function of(roomId: string): string {
    return all()[roomId] ?? "";
  }

  /** Records what is being written, or forgets it once it is empty. */
  function keep(roomId: string, text: string) {
    if (import.meta.server) return;

    try {
      const drafts = all();

      /*
       * An empty draft is removed rather than stored as "".
       *
       * Otherwise every conversation somebody has ever opened accumulates an
       * entry, and the store grows without ever holding anything.
       */
      if (text.trim()) drafts[roomId] = text;
      else delete drafts[roomId];

      localStorage.setItem(storageKey, JSON.stringify(drafts));
    } catch {
      // Full, or a private window. The draft still works for this session; it
      // simply will not survive a reload.
    }
  }

  /** Forgets every draft, for signing out. */
  function forget() {
    if (import.meta.server) return;

    try {
      localStorage.removeItem(storageKey);
    } catch {
      // Nothing to do.
    }
  }

  return { of, keep, forget };
}
