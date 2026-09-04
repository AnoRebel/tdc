/**
 * Turns a server-relative media path into one the browser can fetch.
 *
 * The API returns paths rather than absolute URLs, because a server does not
 * reliably know the address a client reached it on — behind a proxy the Host
 * header is whatever the proxy says. That is right for the server and wrong for
 * an <img src>, which resolves a relative path against the page's own origin.
 * In production those are the same host and it works by accident; in
 * development the app is on one port and the API on another, and every image
 * would 404 against the dev server.
 */
export function useMediaUrl() {
  const apiBase = useRuntimeConfig().public.apiBase ?? "";

  return function mediaUrl(path: string | undefined): string {
    if (!path) return "";
    // Already absolute — a presigned object-store URL, for instance.
    if (/^https?:\/\//i.test(path)) return path;
    return `${apiBase}${path}`;
  };
}
