/**
 * Plain wording for the reasons a report can be filed under.
 *
 * Shared between the dialog somebody files a report in and the queue a
 * moderator reads it from: the same complaint should read the same way to both
 * of them, and two copies of this list drift.
 *
 * The server's list is authoritative — a reason it sends that has no entry
 * here falls back to its own name rather than disappearing from the screen.
 */
const known = new Set(["spam", "harassment", "hate", "violence", "sexual", "illegal", "other"]);

export function reportReasonLabel(reason: string, say: (key: string) => string): string {
  return known.has(reason) ? say(`moderation.reasonLabels.${reason}`) : reason;
}
