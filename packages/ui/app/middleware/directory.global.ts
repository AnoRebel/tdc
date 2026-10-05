/**
 * Keeps people out of directory pages on a server that does not offer one.
 *
 * The sidebar already leaves the directory out when the server does not list
 * it, but a bookmark or an old link still reaches the page, which would then
 * show a form whose every request answers 404. Sent to the start instead: on
 * such a server there is no directory to show, not an empty or broken one.
 *
 * Navigation, not enforcement. The server refuses every directory request
 * while the directory is off, whatever the interface does.
 */
export default defineNuxtRouteMiddleware(async (to) => {
  if (!String(to.name ?? "").startsWith("directory")) return;

  // Both asked for before anything is awaited: an injection after an await in
  // middleware reaches for a context that is no longer there.
  const server = useServerInfo();
  const { $localeRoute: localeRoute } = useI18n();

  await server.load();
  if (server.offers("directory")) return;

  return navigateTo(localeRoute({ name: "index" }), { replace: true });
});
