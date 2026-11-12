import type { LinkPreview } from "@tdc/types";
import { withoutCode } from "#layers/ui/app/lib/formatting";

/**
 * Fetches a summary of a link, once per link per session.
 *
 * The server caches across everyone, so this only avoids repeating the request
 * as a person scrolls a conversation up and down — which is enough to matter,
 * since a busy channel re-renders the same messages constantly.
 *
 * A link with nothing to show is remembered too. Without that, a message
 * linking to a page with no metadata would ask again on every render for an
 * answer that will not change.
 */

/** Shared across components, so two messages linking to the same page ask once. */
const cache = new Map<string, LinkPreview | null>();
const inFlight = new Map<string, Promise<LinkPreview | null>>();

export function useLinkPreview() {
  const api = useApi();

  async function preview(url: string): Promise<LinkPreview | null> {
    if (cache.has(url)) return cache.get(url) ?? null;

    const existing = inFlight.get(url);
    if (existing) return existing;

    const request = api
      .post<LinkPreview | null>("/api/previews", { url })
      .then((result) => {
        // A 204 means there is nothing to show — the operator has previews
        // off, the person has, or the page had no metadata. All three are
        // "no card", and none is an error.
        const value = result && (result.title || result.description) ? result : null;
        cache.set(url, value);
        return value;
      })
      .catch(() => {
        // A failed preview must never break the message it belongs to.
        cache.set(url, null);
        return null;
      })
      .finally(() => {
        inFlight.delete(url);
      });

    inFlight.set(url, request);
    return request;
  }

  return { preview };
}

/**
 * Finds the links in a message.
 *
 * Only the first is previewed. A message full of links would otherwise produce
 * a wall of cards taller than the conversation, and the first is almost always
 * the one being talked about.
 */
export function firstLinkIn(body: string): string | null {
  // A link inside code is an example, not something being shared, and the
  // message does not show it as a link either.
  const match = withoutCode(body).match(/https?:\/\/[^\s<>"']+/);
  if (!match) return null;

  // Trailing punctuation is far more likely to be the sentence's than the
  // URL's: "see https://tdc.or.tz." should not fetch a trailing full stop.
  return match[0].replace(/[.,;:!?)\]}]+$/, "");
}
