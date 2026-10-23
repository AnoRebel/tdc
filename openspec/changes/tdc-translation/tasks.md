Scope note: these tasks implement `messaging/translation` against the running platform. Tasks reference the spec for what to build and `design.md` for how.

**Documentation rule:** the user guide's Translation section, the hosting guide's Translation section, the integration guide's endpoints and the privacy-policy template stay current with the work.

## 1. Server

- [x] 1.1 Migration `000426_translation`: preferences, room rules, connections (sealed key), the shared cache, and the trigger that deletes a redacted message's translations
- [x] 1.2 `translation.*` configuration (instance URL and key, secret key, timeout), validated at startup and redacted; `tdc-server -translation-key`
- [x] 1.3 The `translation` toggle (on), `translation.providers` and `translation.requests_per_hour`
- [x] 1.4 `internal/translation`: LibreTranslate, Google, Microsoft and DeepL over HTTP, batched; AES-GCM sealing bound to account and provider; guarded dialling for everything a member configured; no text in logs or errors
- [x] 1.5 Markdown-aware segments in Go and TypeScript, both held to `translation.cases.json`
- [x] 1.6 Endpoints for preferences, connections, room rules and the proxy, in the API catalogue; the proxy checks the room and each message, counts only provider requests against the hourly limit, and caches by message, language, provider and body hash
- [x] 1.7 API tests against a stand-in provider: default on and sending nothing, switched off, key never returned and not stored as written, visibility (room and per message, removed messages), code and mentions kept, cache and edit invalidation, removal deleting the cache, same-language skip, rate limit, SSRF (literal addresses refused on save, a name resolving to loopback never connected), operator allow-list. Sabotaged: key leaked in the overview, room check removed, per-message room check removed, unguarded dialer, limiter removed — each caught

## 2. Clients

- [x] 2.1 `useTranslation`: overview, effective provider and language, per-message choice over conversation rule over server-wide choice, batching, in-memory results; provider code loaded on first use
- [x] 2.2 On-device provider over the Translator and Language Detector APIs, feature-detected, with the activation a model download needs
- [x] 2.3 `TranslationBody` in conversations and threads; Translate / Show original in the message actions and menu; the header toggle for a conversation
- [x] 2.4 Settings section: provider, language, translate everything, connecting and removing each provider with who sees the text; operator settings grouped on the administration screen
- [x] 2.5 English and Swahili strings; payload budget unchanged
- [x] 2.6 Browser check against a local server and a stand-in LibreTranslate: translate a message, show the original, translate a conversation automatically, connect and remove a key

## 3. Not done

- [ ] 3.1 Rotating `translation.secret_key` without members reconnecting
