/**
 * A short line at the bottom of the screen saying something happened, or did
 * not.
 *
 * For the actions that change something without a visible trace where
 * somebody is looking — a conversation muted from a menu that has closed, a
 * pin the server refused and that has just been put back. One at a time; a
 * newer notice replaces an older one rather than queueing behind it.
 */
export function useNotice() {
  const notice = useState<{ text: string; at: number } | null>("notice", () => null);

  function say(text: string) {
    const at = Date.now();
    notice.value = { text, at };
    setTimeout(() => {
      if (notice.value?.at === at) notice.value = null;
    }, 4000);
  }

  return { notice, say };
}
