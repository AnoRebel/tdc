/**
 * Where media lives, as the two clients disagree about it.
 *
 * The API returns media as server-relative paths, and turning one into
 * something the webview can load is the one media question the clients answer
 * differently. The web client serves its interface from the same origin as the
 * API in production, and from a configured base in development, so a relative
 * path resolves against the page — or against that base. The desktop client's
 * interface is served from the webview's own internal origin, which is not the
 * server at all, so a relative path there resolves to nothing; it has to be
 * told the address of whichever server the person chose.
 *
 * Injected rather than read from runtime config, because runtime config is a
 * web notion. Each application provides the base under this key, the way each
 * provides its transport under apiClientKey.
 */
export const mediaBaseKey = Symbol("tdc.mediaBase") as InjectionKey<string>;

/**
 * The base every media path is resolved against.
 *
 * Empty string when nothing is provided, which is what the web client wants in
 * production: an empty base leaves the path relative, and it resolves against
 * the page's own origin, which is the server. So a missing provider degrades to
 * same-origin rather than failing — correct for the web, and the desktop client
 * always provides one.
 */
export function useMediaBase(): string {
  return inject(mediaBaseKey, "");
}
