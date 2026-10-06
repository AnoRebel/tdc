/**
 * Saving a file the server produces, as the two clients do it differently.
 *
 * On the web the browser holds the session, so a plain link to the endpoint is
 * enough — and better than anything else, because the browser streams the
 * response straight to disk instead of holding it in memory first. On the
 * desktop the session is in the Go client's cookie jar, out of the webview's
 * reach, so the same link would arrive unauthenticated and save a refusal. The
 * desktop client asks Go for the bytes instead and saves those.
 *
 * Injected under this key and required, the way the media fetcher is: the
 * layer never decides on its own how a file reaches the disk, so that no page
 * here quietly assumes the browser's cookie is there to carry.
 */
export type Download = (path: string, filename: string) => Promise<void>;

export const downloadKey = Symbol("tdc.download") as InjectionKey<Download>;

/**
 * How to save a server file, from whichever application is running.
 *
 * Loud when nothing was provided, for the reason useMediaFetch is: a forgotten
 * provider should fail at the page that needed it, not at the first click.
 */
export function useDownload(): Download {
  const provided = inject(downloadKey, null);
  if (!provided) {
    throw new Error(
      "No download has been provided. An application extending @tdc/ui must " +
        "provide one under downloadKey — see the web client's media plugin.",
    );
  }
  return provided;
}
