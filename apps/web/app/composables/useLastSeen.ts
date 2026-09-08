import { remember as rememberIn, type Remembered } from "~/lib/lastseen";

/*
 * storageKey holds conversations across a restart.
 *
 * The point of keeping them at all is the visit that starts with no
 * connection, and that visit is usually a fresh page load.
 */
const storageKey = "tdc.lastseen";

/**
 * What each conversation looked like when the server was last reached.
 *
 * Read when a timeline cannot be fetched, so that opening the application on a
 * train shows the conversation rather than an empty room. What is shown is
 * labelled as last known rather than presented as current: a stale timeline
 * passed off as live would have somebody replying to a conversation that has
 * moved on.
 */
export function useLastSeen<T, R = unknown>() {
  function all(): Record<string, Remembered<T, R>> {
    if (import.meta.server) return {};

    try {
      const stored = localStorage.getItem(storageKey);
      if (!stored) return {};

      const parsed: unknown = JSON.parse(stored);
      return typeof parsed === "object" && parsed !== null
        ? (parsed as Record<string, Remembered<T, R>>)
        : {};
    } catch {
      // Unreadable or written by a version that shaped it differently.
      // Nothing here is worth failing over: it is a convenience, not a record.
      return {};
    }
  }

  /** What was last seen in one conversation, if anything. */
  function of(roomId: string): Remembered<T, R> | null {
    return all()[roomId] ?? null;
  }

  /** Records what a conversation looks like now. */
  function keep(roomId: string, messages: T[], room?: R) {
    if (import.meta.server) return;

    try {
      localStorage.setItem(
        storageKey,
        JSON.stringify(rememberIn(all(), roomId, messages, Date.now(), room)),
      );
    } catch {
      // Full, or refused. The application still works; it simply will not have
      // anything to show the next time it opens without a connection.
    }
  }

  /** Forgets everything, for signing out. */
  function forget() {
    if (import.meta.server) return;

    try {
      localStorage.removeItem(storageKey);
      // The sidebar's list too: it is the same person's conversations, kept
      // under its own key because the layout writes it.
      localStorage.removeItem("tdc.lastseen.rooms");
    } catch {
      // Nothing to do. The next sign-in overwrites it anyway.
    }
  }

  return { of, keep, forget };
}
