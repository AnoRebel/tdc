import type { GifSearchResults } from "@tdc/types";

/**
 * Searching a third-party GIF library, when this server offers one.
 *
 * Off unless an operator configured a provider and their own key, so the
 * control is hidden rather than offered and failing. Sending a GIF as an
 * ordinary attachment works regardless and needs none of this.
 */
export function useGifs() {
  const api = useApi();
  const { offers } = useServerInfo();

  /** Whether this server offers search at all. */
  const available = computed(() => offers("gif-search"));

  const results = ref<GifSearchResults["results"]>([]);
  const provider = ref("");
  const searching = ref(false);
  const failed = ref(false);

  let issued = 0;
  let settled = 0;
  let timer: ReturnType<typeof setTimeout> | undefined;

  /**
   * Runs a search, discarding a reply that has been overtaken.
   *
   * Somebody typing "dancing" fires several searches, and without a sequence
   * the answer to "danc" can arrive after the answer to "dancing" and replace
   * it.
   */
  async function run(query: string) {
    if (!available.value) return;

    const sequence = ++issued;
    searching.value = true;
    failed.value = false;

    try {
      const found = await api.get<GifSearchResults>(
        `/api/gifs?q=${encodeURIComponent(query.trim())}`,
      );

      if (sequence < settled) return;
      settled = sequence;

      results.value = found.results;
      provider.value = found.provider;
    } catch {
      if (sequence < settled) return;
      settled = sequence;

      results.value = [];
      failed.value = true;
    } finally {
      if (sequence === issued) searching.value = false;
    }
  }

  /**
   * Searches after a pause.
   *
   * Longer than the local emoji search, because each of these is a request to
   * somebody else's API against the operator's quota — and a per-keystroke
   * request would spend it on words nobody finished typing.
   */
  function search(query: string) {
    if (timer) clearTimeout(timer);
    timer = setTimeout(() => void run(query), 350);
  }

  onScopeDispose(() => {
    if (timer) clearTimeout(timer);
  });

  return { available, results, provider, searching, failed, search, run };
}
