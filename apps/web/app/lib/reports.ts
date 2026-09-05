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
const wording: Record<string, string> = {
  spam: "Spam or advertising",
  harassment: "Harassment or bullying",
  hate: "Hate speech",
  violence: "Violence or threats",
  sexual: "Sexual content",
  illegal: "Something illegal",
  other: "Something else",
};

export function reportReasonLabel(reason: string): string {
  return wording[reason] ?? reason;
}
