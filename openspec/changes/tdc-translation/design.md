## Context

Messages are events; an edit is a later `message.edited` event that replaces the body, and a removal redacts the event in place (`content` emptied, `redacted_at` set). Who may read a room is decided in one place, `rooms.Service.Visible`. Runtime policy lives in `settings` (feature switches with `defineFeature`, keys with `define`); secrets an operator holds live in the configuration file and are redacted from `/api/admin/configuration`. Outbound requests that a member can influence use `unfurl.DialContext`, which refuses private addresses. The GIF search (`internal/gifs`) is the existing example of a third-party API proxied through the server. Message bodies are rendered by `packages/ui/app/lib/render-message.ts` from the Markdown their author typed.

## Decisions

### Providers

| Provider | Where it runs | Who sees the text | Cost | Key |
|---|---|---|---|---|
| `device` — the browser's Translator and Language Detector APIs | the reader's browser | nobody; the model runs on the device | free | none |
| `libretranslate` — the operator's instance | this server → the instance | whoever runs the instance (usually the operator) | free | operator's, optional |
| `libretranslate` — the person's own instance or a public one | this server → that instance | whoever runs that instance | free or paid | person's, optional |
| `google` — Cloud Translation v2 | this server → Google | Google | paid, with a free tier | person's |
| `microsoft` — Translator v3 ("Bing") | this server → Microsoft | Microsoft | paid, with a free tier | person's, plus region |
| `deepl` — DeepL API v2 (Free keys end in `:fx`) | this server → DeepL | DeepL | paid, with a free tier | person's |

One Go interface (`translation.Service.Translate(provider, credentials, texts, target)`) covers the four HTTP providers; the client has the same shape for `device`, so the rest of the client does not care where a translation came from.

### The default free option

`device` where the browser has it, otherwise the operator's LibreTranslate instance where one is configured, otherwise nothing until the person connects a provider. A person's chosen provider always wins.

On-device first because it is the only option that is both free and private: nothing about the message leaves the reader's machine (the browser downloads a language model once, from its vendor, which says nothing about the message). It exists today only in Chromium-based desktop browsers with the models available (Chrome 138 and later), not in Firefox, Safari, WebKitGTK or — at the time of writing — WebView2, so it cannot be the only answer. LibreTranslate is the free fallback because it is open source and an operator can run it next to TDC, which keeps message text on infrastructure they already answer for; public instances exist but are rate-limited and see the text, which is why a public instance is something a person chooses rather than a default. Commercial providers are never a default: somebody else's message would go to a company because of a choice its author never saw.

### The feature switch is on by default

`translation` defaults **on**, because switching it on changes nothing about where text goes: on-device translation sends nothing, the operator's instance exists only if the operator configured it, and every other provider needs a person to connect it with their own key. An operator who does not want members sending each other's messages to commercial providers removes them from `translation.providers`; one who wants no translation at all switches the feature off, and every endpoint then answers 404 and the client shows nothing.

### Settings

Configuration (file or `TDC_TRANSLATION_*`):

- `translation.libretranslate_url` — the operator's instance, reached with an ordinary dialer (it is the operator's own, often on a private network beside TDC).
- `translation.libretranslate_api_key` — secret, redacted from the configuration inspection.
- `translation.secret_key` — 32 bytes, base64; encrypts personal keys. `tdc-server -translation-key` prints a fresh one. Without it, personal keys cannot be saved (the client is told, and says so); on-device and the operator's instance still work.
- `translation.timeout` — a provider call's bound, 10s by default.

Runtime settings (admin screen):

- `translation.providers` — the providers members may use, comma-separated; all five by default.
- `translation.requests_per_hour` — per person, counting only requests that reach a provider (cache hits are free); 120 by default.

### Per-person data

- `translation_preferences` (user → provider, target language, translate this server automatically). An empty provider means "the default" above; an empty language means the person's interface language.
- `translation_room_rules` (user, room → translate automatically or not), overriding the server-wide choice for one conversation.
- `translation_connections` (user, provider → sealed key, endpoint, region). The key is sealed with AES-256-GCM under `translation.secret_key`, with the user and provider as additional data so a sealed key copied to another row will not open. Nothing ever reads it back to a client: responses carry `hasKey`, never the key or any part of it. Removing a connection deletes the row.

Preferences belong to the account on that server. The desktop application holds several servers, each with its own account, so it has one set per server without doing anything special.

### The proxy

`POST /api/rooms/:id/translations` takes up to 50 message identifiers, a target language and a provider. It:

1. checks the room with `Visible` — a room the person may not read is 404, as everywhere;
2. for each message, takes it only if it is in that room, is a `message.sent`, is not redacted, and resolves its current body through the newest unredacted edit; anything else is returned in `missing` without saying why;
3. looks each body up in the cache by (message, target, provider, SHA-256 of the body);
4. if anything is left, counts one request against the person's hourly limit (429 with Retry-After when spent), then sends the translatable segments of every remaining body to the provider in one batched call;
5. stores the results and returns them.

The provider HTTP client refuses redirects, bounds responses at 2 MB, and never wraps transport errors (Go's include the URL, which for Google carries the key). Nothing logs message text or provider response bodies, only provider name and status. Personal LibreTranslate endpoints must be `https`, may not be a literal private address (checked when saved), and are dialled through `unfurl.DialContext` (checked again when used, which catches a name that resolves somewhere private). Fixed provider hosts use the same guarded dialer.

### The cache

`message_translations` (message, source event, target, provider, body hash → translated body, detected source language). Shared between people: a translation is of a message every requester was just shown to be allowed to read, and sharing it is what makes auto-translating a busy channel affordable. Invalidated three ways: a different body hash misses (an edit); older rows for the same message are deleted when a new body is translated; and a trigger on `events` deletes every row whose message or source event is redacted, so a removed message's translation does not outlive it — whatever removed it (the author, a moderator, retention, erasure).

### Markdown-aware segments

A body is split into segments, each either sent for translation or kept verbatim: fenced code blocks; inline code; link targets (the `](…)` of a Markdown link — the label is translated); bare URLs; email addresses; `@mentions` and `#channels`; `:emoji:` shortcodes; line-leading list, quote and heading markers; whitespace at segment edges; and any run with no letters. Kept segments never leave the server (or the device), and come back exactly as written. Each line is segmented separately, so line structure survives. The same rules are implemented in Go (`translation.Split`) and TypeScript (`lib/translation-segments.ts`) and both are tested against `packages/ui/app/lib/translation.cases.json`, as message plain text already is.

The cost is context: "use `make` here" is sent as two pieces. Chat messages with inline code are a minority, and a provider rewriting a command or a mention would be the worse failure.

### Language detection

Providers report the source language with the translation. When it matches the target (by primary subtag), the message is marked `skipped` and the client shows it as it is, without a "translated" label. Where the browser has a Language Detector, the client checks first and does not ask at all for a message already in the target language — which saves a request, and with `device` is the only detection there is.

### API

All require authentication and the `translation` feature.

- `GET /api/me/translation` → `{ providers: [{ name, allowed, connected, hasKey, shared, endpoint, region }], canStoreKeys, defaultLanguage, preferences: { provider, targetLanguage, autoTranslate }, rooms: [{ roomId, autoTranslate }] }`. `shared` is true for `libretranslate` when the operator configured an instance.
- `PUT /api/me/translation/preferences` `{ provider, targetLanguage, autoTranslate }` → 204.
- `PUT /api/me/translation/connections/:provider` `{ apiKey, endpoint, region }` → 204. A blank `apiKey` on an existing connection keeps the saved key, so changing a region does not mean pasting the key again.
- `DELETE /api/me/translation/connections/:provider` → 204.
- `PUT /api/me/translation/rooms/:id` `{ autoTranslate }` → 204; `DELETE` returns the room to the server-wide choice.
- `POST /api/rooms/:id/translations` `{ messages: [id], targetLanguage, provider }` → `{ provider, targetLanguage, translations: [{ messageId, body, sourceLanguage, skipped }], missing: [id] }`. 400 for a bad language or the `device` provider, 403 for a provider the operator does not allow, 409 when the provider needs a connection the person has not made, 429 when the hourly limit is spent, 502 when the provider fails.

### Client

`useTranslation` (shared layer) holds the overview, resolves the effective provider and language, decides per message whether it is shown translated (a per-message choice, else the conversation's rule, else the server-wide choice; never the reader's own messages), batches requests per room, and caches results in memory by message, body, language and provider. The provider code — the on-device wrapper, the segmenter and the settings section — is imported on first use, so a person who never translates downloads none of it.

`TranslationBody` replaces `MessageBody` in the conversation and in threads: it renders the translated Markdown through the same renderer, and below it "Translated from Swahili · Show original". The message's hover actions and context menu gain Translate / Show original; the conversation header gains a toggle for translating that conversation automatically. The settings page gains a Translation section: provider, language, translate everything on this server, and connecting or removing each provider, with a sentence per provider saying who sees the text.

On-device translation needs a click the first time a language model is downloaded (the browser requires user activation for that); until then an automatic translation that would need one waits, and the message's own Translate button is that click.

### Desktop

The desktop application uses the shared layer unchanged. WebView2 is Chromium-based and may expose the on-device APIs in future; WKWebView and WebKitGTK do not. The client feature-detects (`"Translator" in self && "LanguageDetector" in self`, then `availability()`), and falls back to the server providers, so every desktop build translates through the server where the browser cannot.

## Risks

- **Another person's message goes to a third party.** The reader chooses that, as they could by pasting it; the guide says so per provider, the operator can deny providers, and the privacy-policy template has a paragraph for it.
- **A lost `translation.secret_key` makes saved keys unreadable.** They are then refused as if absent and people reconnect; nothing else depends on the key. Rotation is not built: the guide says to remove and reconnect.
- **Public LibreTranslate instances rate-limit and change.** None is configured by default; a person who adds one sees its errors as "the provider could not be reached".
