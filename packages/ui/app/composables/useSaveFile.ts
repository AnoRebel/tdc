/**
 * Saving a file made on this device — a call's recording — to disk.
 *
 * Not a server file, so useDownload does not fit: there is nothing to fetch.
 * The web hands the bytes to the browser as a download. The desktop's webview
 * does not save downloads on every platform, so Go asks where with the
 * system's own dialog and writes the bytes there, a piece at a time.
 *
 * Resolves false when somebody closes the dialog without choosing.
 */
export type SaveFile = (file: Blob, filename: string) => Promise<boolean>;

export const saveFileKey = Symbol("tdc.saveFile") as InjectionKey<SaveFile>;

/** How to save a file made here, from whichever application is running. */
export function useSaveFile(): SaveFile {
  const provided = inject(saveFileKey, null);
  if (!provided) {
    throw new Error(
      "No file saver has been provided. An application extending @tdc/ui must " +
        "provide one under saveFileKey — see the web client's media plugin.",
    );
  }
  return provided;
}
