import type { SearchResults } from "@tdc/types";

/** Empty results, used before a search and whenever the box is cleared. */
function empty(): SearchResults {
  return { messages: [], rooms: [], people: [] };
}

/**
 * Searching messages, conversations and people.
 *
 * Debounced, and every request supersedes the one before it: somebody typing
 * "general" fires eight searches, and without this the answer to "gene" can
 * arrive after the answer to "general" and overwrite it. The sequence number
 * is what makes a late reply harmless — it is discarded rather than raced.
 */
export function useSearch() {
  const api = useApi();

  const query = ref("");
  const results = ref<SearchResults>(empty());
  const searching = ref(false);
  const failed = ref(false);

  let timer: ReturnType<typeof setTimeout> | undefined;
  let issued = 0;
  let settled = 0;

  async function run(text: string, options: { messages?: boolean } = {}) {
    const trimmed = text.trim();
    if (!trimmed) {
      results.value = empty();
      searching.value = false;
      failed.value = false;
      return;
    }

    const sequence = ++issued;
    searching.value = true;
    failed.value = false;

    const parameters = new URLSearchParams({ q: trimmed });
    if (options.messages === false) parameters.set("messages", "false");

    try {
      const found = await api.get<SearchResults>(`/api/search/everything?${parameters}`);

      // A reply that has been overtaken is dropped rather than shown. Without
      // this, results for an earlier prefix can land last and replace the
      // answer to what was actually typed.
      if (sequence < settled) return;
      settled = sequence;

      results.value = found;
    } catch {
      if (sequence < settled) return;
      settled = sequence;

      // Cleared rather than left stale: showing results for a previous term
      // beside the current one is worse than showing nothing.
      results.value = empty();
      failed.value = true;
    } finally {
      // Only the newest request may turn the spinner off, or an overtaken one
      // ends it while the current search is still running.
      if (sequence === issued) searching.value = false;
    }
  }

  /** Searches after a pause, so typing does not send a request per keystroke. */
  function search(text: string, options: { messages?: boolean; delay?: number } = {}) {
    query.value = text;
    if (timer) clearTimeout(timer);

    // Clearing the box empties the results at once: waiting would leave stale
    // results under an empty field, which reads as a broken search.
    if (!text.trim()) {
      void run("");
      return;
    }

    timer = setTimeout(() => void run(text, options), options.delay ?? 180);
  }

  function clear() {
    if (timer) clearTimeout(timer);
    query.value = "";
    results.value = empty();
    searching.value = false;
    failed.value = false;
  }

  const total = computed(
    () => results.value.messages.length + results.value.rooms.length + results.value.people.length,
  );

  const isEmpty = computed(
    () => query.value.trim().length > 0 && !searching.value && total.value === 0,
  );

  onScopeDispose(() => {
    if (timer) clearTimeout(timer);
  });

  return { query, results, searching, failed, total, isEmpty, search, clear };
}
