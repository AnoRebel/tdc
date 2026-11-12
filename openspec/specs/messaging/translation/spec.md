## Purpose

Defines inline message translation: who may translate what, through which providers, what each provider sees, and how the keys that pay for them are kept.

## Requirements

### Requirement: Translation is optional and per person

A server SHALL offer message translation behind the `translation` capability toggle, on by default, and SHALL advertise it to clients only while it is on. Switching it on MUST NOT by itself send any message text off the server or the reader's device.

Each person SHALL choose a provider, a target language, and whether messages are translated automatically — everywhere on the server, in chosen conversations, or only on demand per message. A person's own messages MUST NOT be translated automatically. Preferences SHALL belong to the person's account on that server.

#### Scenario: Translation switched off

- **WHEN** an operator switches `translation` off
- **THEN** every translation endpoint answers as if it did not exist
- **AND** clients show no translation controls

#### Scenario: A conversation's rule overrides the server-wide choice

- **WHEN** a person translates the server automatically but turns translation off for one conversation
- **THEN** messages in that conversation are shown as written unless the person translates one

### Requirement: Providers and the free default

The platform SHALL support on-device translation in the reader's browser where the browser provides it, LibreTranslate (an instance the operator configures, or one the person connects), Google Cloud Translation, Microsoft Translator and DeepL. When a person has chosen no provider, the default SHALL be on-device translation where supported, otherwise the operator's LibreTranslate instance where configured, and never a commercial provider the person has not connected.

The operator SHALL be able to restrict which providers members may use. Who sees message text for each provider SHALL be stated in the user guide and in the settings interface.

#### Scenario: A denied provider is refused

- **WHEN** a provider is not in the operator's allowed list
- **THEN** connecting it and translating with it are refused

#### Scenario: Unsupported browser falls back

- **WHEN** a browser or desktop webview has no on-device translator
- **THEN** the client uses the next available provider instead

### Requirement: Personal keys are sealed and never disclosed

A key a person connects SHALL be stored encrypted with a key held in the server's configuration, bound to that person and provider, and MUST NOT be returned to any client, in whole or in part, after it is saved. A person SHALL be able to replace and remove it.

#### Scenario: A saved key is not shown

- **WHEN** a person connects a provider with a key and then reads their translation settings
- **THEN** the response says a key is saved and contains no part of it

### Requirement: The translation proxy

The server SHALL translate, through a key-based or LibreTranslate provider, only messages the requesting person may read: messages in a room they can see, not removed, using the message's current body. It MUST refuse to connect to private network addresses for any address a member supplied, MUST NOT log message text or a provider's response, SHALL limit each person's provider requests per hour, and SHALL cache translations per message, target language and provider, discarding them when the message is edited or removed.

#### Scenario: A message the person cannot read is not translated

- **WHEN** somebody asks to translate a message in a private room they are not in, or names it through another room
- **THEN** it is not sent to any provider and nothing about it is returned

#### Scenario: A member's endpoint resolves to a private address

- **WHEN** a member connects a LibreTranslate address whose name resolves to this server's own network
- **THEN** no connection is made to it

#### Scenario: Cached translations are free and follow edits

- **WHEN** a translated message is asked for again
- **THEN** no provider is called and the person's allowance is not spent
- **AND** after the message is edited the new words are translated and the old translation is discarded

### Requirement: Markdown-aware, inline display

Translation SHALL leave code, mentions, channel references, links, email addresses and emoji shortcodes exactly as written and send none of them to the provider. A translated message SHALL say what language it was translated from and offer to show the original; a message already in the target language SHALL be shown as written. Messages in threads SHALL behave the same.

#### Scenario: Code survives translation

- **WHEN** a message containing inline code and a mention is translated
- **THEN** the code and the mention appear unchanged in the translation and were not sent to the provider
