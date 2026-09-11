import * as Api from "#bindings/api";

/**
 * Sends a visitor to the step they are missing: a server, or signing in.
 *
 * Two gates rather than the web's one. The web is served from a server and so
 * always has one; the desktop client is installed once and pointed at whichever
 * server its owner belongs to, so before anything else it needs an address. Only
 * once a server is chosen does signing in mean anything.
 *
 * This is navigation, not enforcement: the server refuses every unauthorised
 * request on its own, so a visitor who skips the interface gains nothing.
 */
export default defineNuxtRouteMiddleware(async (to) => {
  /*
   * Asked for before anything is awaited.
   *
   * useAuth injects the transport, and an injection needs the Nuxt context,
   * which is only reliably present in the synchronous part of a middleware.
   * Calling it after awaiting the current server threw "no API client has been
   * provided" — from a client that had provided one perfectly well.
   */
  const { signedIn, ready, refresh } = useAuth();

  // No server chosen yet — that is the first thing to fix, before any thought
  // of a session.
  const current = await Api.Service.CurrentServer();
  if (!current) {
    return navigateTo("/servers");
  }

  // On a full load the session has not been resolved yet; without this a signed
  // in person opening a conversation directly would be bounced to sign in.
  if (!ready.value) {
    await refresh();
  }

  if (signedIn.value) return;

  return navigateTo({ path: "/signin", query: { next: to.fullPath } });
});
