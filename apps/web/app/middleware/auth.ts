/**
 * Sends an unauthenticated visitor to sign in.
 *
 * This is navigation, not enforcement: the server refuses every unauthorised
 * request independently, so a visitor who skips the interface gains nothing.
 */
export default defineNuxtRouteMiddleware(async (to) => {
  const { signedIn, ready, refresh, setupStatus } = useAuth();

  // On a full page load, route middleware runs before the application has
  // resolved the session. Without this, opening a protected page directly
  // would bounce a signed-in person to sign in.
  if (!ready.value) {
    await refresh();
  }

  if (signedIn.value) {
    return;
  }

  /*
   * Whether this server has an administrator yet.
   *
   * Guarded because it runs on a page load that may have no network. An
   * unhandled rejection in route middleware is an error page rather than an
   * application, which would mean a phone with no signal showing a stack
   * trace where it should show the last conversation.
   */
  try {
    const status = await setupStatus();
    if (status.setupRequired) {
      return navigateTo("/setup");
    }
  } catch {
    // Unknown. Sending somebody to sign in is the safe half of the guess: the
    // sign-in page works offline and says so, and setup is a one-off that can
    // wait for a connection.
  }

  return navigateTo({ path: "/signin", query: { next: to.fullPath } });
});
