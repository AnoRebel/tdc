## Why

Messages were plain text: links were found and made clickable, and nothing else. For a developer community that is the wrong default — code pasted into a conversation loses its indentation's meaning, wraps mid-line, and cannot be copied cleanly, and people arriving from Slack, Discord, Telegram or GitHub expect `**bold**` and fenced code blocks to work. Markdown was already rendered for attached text files; messages were the gap.

## What Changes

- **Rendering.** Message bodies render a safe Markdown subset in the shared layer, so the web and desktop clients both have it: bold, italic, strikethrough (two tildes only), inline code, fenced code blocks with a language label, a copy button, horizontal scrolling and lazily loaded syntax highlighting, block quotes, lists, and `[text](address)` links to `http`, `https` and `mailto` addresses. Headings render as a bold line. Raw HTML is text; images are a link to their address and never fetched; tables, rules, indented code, setext headings and link definitions are left as text, since each is typed by accident more often than on purpose. The renderer is the attachment viewer's (marked), configured separately, followed by DOMPurify limited to the dozen tags the subset produces. Existing link detection (addresses, emails, phone numbers) runs over the text that remains, never inside code. A one-paragraph body renders inline, so plain messages look exactly as before. Rendered bodies are memoised.
- **Stored body is unchanged in kind.** It is the Markdown source; no new field, no migration, no change on the wire or across federation.
- **Plain text where formatting cannot be shown.** One set of stripping rules, implemented in the server (`internal/markup`), the web layer (`lib/formatting.ts`) and the desktop application's banner code, all tested against one shared case file. Applied to notification previews, push payloads, desktop banners, the search index (so excerpts and highlights are plain), and in the interface to the pinned list, delete and report confirmations, the waiting-message label, the quoted origin of an issue and an issue's title made from a message. A link preview ignores links inside code. Mentions inside code no longer notify; a mention made bold still does.
- **Composer.** Ctrl/⌘ B, I, Shift X, E and Shift C wrap or unwrap the selection; an optional toolbar (remembered per browser) with a syntax reference; Enter inside an open code fence starts a new line. Editing in place uses a multi-line field so a code block survives an edit.
- **Preference.** "Show formatting in messages", on by default, beside motion and density; off shows the source with links still found.

**Out of scope:** a rich-text (WYSIWYG) composer; tables; images by Markdown; rendering formatting in email (digests contain no message text).

## Capabilities

### Modified Capabilities

- `messaging/channels`: a new "Message formatting" requirement.
- `clients/web`: a new "Composing formatted messages" requirement.

## Impact

- **Dependencies:** `@speed-highlight/core` (CC0) as a direct dependency of the shared layer; it was already in the tree through Nuxt's development tooling. Loaded only when a labelled code block is on screen. marked and DOMPurify were already dependencies.
- **Payload:** the parser loads with the conversation, not before it; the first-visit budget is unchanged in substance (blocking 218.1 → 218.2 KB, prefetched 84.6 → 84.7 KB).
- **Integrations:** bots and webhooks may send Markdown; it renders. Bodies delivered to outgoing webhooks are the source text.
- **Search:** messages are indexed without their markers; messages indexed before this change are unaffected, since nothing could be formatted then.
