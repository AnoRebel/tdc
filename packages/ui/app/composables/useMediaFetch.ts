/**
 * Fetching the bytes of a piece of media, as the two clients do it differently.
 *
 * Most media is loaded by the webview itself, through an <img> or <video> src —
 * a URL the browser fetches with the credentials it holds. A few things need
 * the bytes in hand rather than in an element: a text file to decode and show,
 * for one. On the web that is a fetch with the session cookie the browser
 * carries. On the desktop the session is not in the webview at all — it is in
 * the Go client's cookie jar, kept out of reach of any script — so a fetch from
 * here would go out unauthenticated and be refused. The desktop client provides
 * a version that asks Go to fetch it instead.
 *
 * Injected under this key, and required rather than defaulted: the layer does
 * not fetch on its own, so that no file here quietly assumes the web client's
 * transport. Each application provides one, the way each provides useApi's.
 */
export type MediaFetch = (path: string) => Promise<ArrayBuffer>;

export const mediaFetchKey = Symbol("tdc.mediaFetch") as InjectionKey<MediaFetch>;

/**
 * How to fetch media bytes, from whichever application is running.
 *
 * Loud when nothing was provided, and early — the same failure useApi gives,
 * for the same reason: an application that forgot leaves a component here
 * failing on its first read, some distance from the omission.
 */
export function useMediaFetch(): MediaFetch {
  const provided = inject(mediaFetchKey, null);
  if (!provided) {
    throw new Error(
      "No media fetcher has been provided. An application extending @tdc/ui " +
        "must provide one under mediaFetchKey — see the web client's media plugin.",
    );
  }
  return provided;
}
