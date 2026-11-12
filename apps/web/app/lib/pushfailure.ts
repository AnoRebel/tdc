/**
 * What to tell somebody when subscribing to notifications fails.
 *
 * Extracted from the composable because it is a decision rather than a
 * side effect, and because the case that matters — a browser whose push
 * service will not register it — cannot be reproduced in a test by any other
 * means. The browser reports it as "Registration failed - push service
 * error", which names neither the problem nor anything the reader can do.
 */
export function subscribeFailure(cause: unknown, say: (key: string) => string): string {
  /*
   * The browser could not register with its own push service.
   *
   * Chromium builds without Google's credentials — and any browser behind a
   * network that cannot reach its push service — always fail this way. Push
   * is unavailable in that browser, which is worth saying plainly rather than
   * leaving somebody to retry a button that cannot work.
   */
  if (cause instanceof Error && cause.name === "AbortError") {
    return say("notification.browserCannot");
  }

  /*
   * The key was rejected, almost always because an operator changed the VAPID
   * pair while devices were subscribed to the old one. Worth saying, because
   * the remedy is a step the reader can take.
   */
  if (cause instanceof Error && cause.name === "InvalidStateError") {
    return say("notification.staleKey");
  }

  /*
   * Anything else keeps the browser's own words. They are usually poor, but
   * inventing a friendlier message for a failure nobody has characterised
   * would be guessing at what went wrong.
   */
  return cause instanceof Error ? cause.message : say("notification.couldNotSubscribe");
}
