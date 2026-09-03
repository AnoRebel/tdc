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

  const status = await setupStatus();
  if (status.setupRequired) {
    return navigateTo("/setup");
  }

  return navigateTo({ path: "/signin", query: { next: to.fullPath } });
});
