/**
 * Restores messages that were waiting when the application last closed.
 *
 * A plugin rather than a page hook: the queue belongs to the session, not to a
 * conversation. Somebody who composed a message on a train, closed the tab and
 * came back still wrote that message, and it should still be waiting.
 */
export default defineNuxtPlugin(() => {
  useOutbox().restore();
});
