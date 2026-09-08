/**
 * The last state of a conversation this browser saw.
 *
 * Kept so that opening the application with no connection shows what was
 * there rather than an empty room. Deliberately not called a cache: it is a
 * record of what was true when the server was last reached, and the interface
 * says so rather than presenting it as current.
 */

/*
 * roomLimit bounds how many conversations are kept.
 *
 * Somebody scrolls back through the few they are actually in. Keeping every
 * channel they have ever opened would fill a phone's storage with rooms they
 * looked at once.
 */
export const roomLimit = 20;

/*
 * messageLimit bounds each one.
 *
 * Enough to read back through a conversation, not enough to matter. A
 * thousand messages of history is not what somebody needs on a train; the
 * recent exchange is.
 */
export const messageLimit = 100;

/** What was stored for one conversation. */
export interface Remembered<T, R = unknown> {
  messages: T[];
  /*
   * The conversation itself — its name, topic, whether this person is a
   * member. Without it an offline timeline appears under a blank header,
   * which reads as a different kind of failure than it is.
   */
  room?: R;
  /** When the server last confirmed this, so the interface can say. */
  at: number;
}

/**
 * Trims a timeline to what is worth keeping.
 *
 * The end rather than the beginning: a conversation is read from its most
 * recent message backwards, and the oldest hundred would be the part nobody
 * opens the application to see.
 */
export function trim<T>(messages: T[]): T[] {
  return messages.slice(-messageLimit);
}

/**
 * The store after remembering one conversation.
 *
 * Least recently seen conversations are dropped once there are too many.
 * Returns a new object rather than mutating, so a caller holding the old one
 * is not changed underneath it.
 */
export function remember<T, R>(
  store: Record<string, Remembered<T, R>>,
  roomId: string,
  messages: T[],
  at: number,
  room?: R,
): Record<string, Remembered<T, R>> {
  const next = { ...store, [roomId]: { messages: trim(messages), room, at } };

  const keys = Object.keys(next);
  if (keys.length <= roomLimit) return next;

  /*
   * Oldest first, so the ones dropped are the ones least recently looked at.
   * Dropping arbitrarily would eventually discard the conversation somebody
   * uses every day in favour of one they opened once.
   */
  const byAge = keys.sort((a, b) => next[a]!.at - next[b]!.at);
  for (const key of byAge.slice(0, keys.length - roomLimit)) delete next[key];

  return next;
}
