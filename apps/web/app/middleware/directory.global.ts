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

  const server = useServerInfo();
  await server.load();
  if (server.offers("directory")) return;

  const { $localeRoute: localeRoute } = useI18n();
  return navigateTo(localeRoute({ name: "index" }), { replace: true });
});
