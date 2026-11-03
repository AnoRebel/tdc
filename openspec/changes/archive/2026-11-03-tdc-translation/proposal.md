## Why

A community that meets in TDC writes in more than one language. A Tanzanian chapter writes in Swahili and English in the same channel; a regional server has members writing French, Arabic or Amharic. Today somebody who cannot read a message copies it into a translation site, which sends it — and whatever else they pasted — to whoever runs that site, and takes them out of the conversation to do it.

## What Changes

Optional, per-person, inline message translation.

- **Several providers behind one interface.** The browser's own on-device translator where it exists (Chrome's Translator and Language Detector APIs: free, and nothing leaves the device); LibreTranslate (free and open, self-hosted by the operator or by the person, or a public instance); Google Cloud Translation; Microsoft Translator; DeepL. The last three, and a personal LibreTranslate, are connected by each person with their own key.
- **A free default.** On-device where the browser has it; otherwise the operator's LibreTranslate instance where the operator configured one. Nobody's text goes to a third party unless they, or the operator, chose a provider that sends it.
- **Per person, per server.** Each person chooses a provider, a target language, and when to translate: everything on this server automatically, particular conversations automatically, or one message at a time on demand. Preferences live with the account on each server, so the desktop application — which holds several servers — keeps one set per server.
- **Inline.** A message has a translate toggle; a translated message says "Translated from X · Show original". A conversation's header has a toggle for translating that conversation automatically. Threads behave the same. Code, mentions, links and addresses are never sent for translation and come back exactly as written.
- **Keys stay on the server.** Personal keys are encrypted at rest with an operator-held key, never returned to any client after they are saved, and can be removed. Key-based providers are called through a server proxy that checks the person may read every message they ask about, is safe against being pointed at the server's own network, is rate-limited per person, never logs message text, and caches translations per message, language and provider, dropping them when the message is edited or removed.
- **Operator controls.** A `translation` feature switch (on by default — see design), which providers are allowed, an operator-provided LibreTranslate instance everybody may use, and a per-person rate limit.

## Capabilities

### New Capabilities

- `messaging/translation`: inline message translation, its providers, the proxy, and its privacy guarantees.

### Modified Capabilities

- `platform/server-config`: the `translation` switch and its settings.

## Impact

- **Dependencies:** none. Providers are reached over their published HTTP APIs; no provider SDK and no LibreTranslate code is vendored.
- **Operators:** storing personal keys needs `translation.secret_key`; without it people can still use on-device translation and the operator's instance. The hosting guide says so, and the privacy-policy template gains a paragraph for translation.
- **Privacy:** which provider sees message text is stated in the user guide, per provider, and on the settings screen.
- **Payload:** translation code is loaded only when a translation is first asked for or the settings section is opened.
