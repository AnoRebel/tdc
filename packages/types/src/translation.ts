/**
 * Translating messages inline.
 *
 * `device` runs in the reader's own browser and never reaches the server; the
 * others are called by the server on the reader's behalf, with the operator's
 * LibreTranslate instance or a key the reader connected.
 */
export type TranslationProvider = "device" | "libretranslate" | "google" | "microsoft" | "deepl";

/** One provider, as this person may use it. */
export interface TranslationProviderState {
  name: TranslationProvider;
  /** Whether the operator lets members use it. */
  allowed: boolean;
  /** Whether this person has connected it. */
  connected: boolean;
  /** Whether a key is saved for it. The key itself is never sent back. */
  hasKey: boolean;
  /** For libretranslate: the operator runs an instance everybody may use. */
  shared: boolean;
  /** A personal LibreTranslate instance's address. */
  endpoint?: string;
  /** Microsoft's resource region, where the key needs one. */
  region?: string;
}

/** What somebody chose. Empty strings mean "the default". */
export interface TranslationPreferences {
  provider: TranslationProvider | "";
  targetLanguage: string;
  /** Translate everything on this server automatically. */
  autoTranslate: boolean;
}

/** A conversation translated, or not, regardless of the server-wide choice. */
export interface TranslationRoomRule {
  roomId: string;
  autoTranslate: boolean;
}

/** Everything a client needs to offer translation. */
export interface TranslationOverview {
  providers: TranslationProviderState[];
  /** False when the server has no key to seal personal keys with. */
  canStoreKeys: boolean;
  defaultLanguage: string;
  preferences: TranslationPreferences;
  rooms: TranslationRoomRule[];
}

/** What connecting a provider sends. A blank key keeps the saved one. */
export interface TranslationConnection {
  apiKey: string;
  endpoint: string;
  region: string;
}

/** One message, translated. */
export interface TranslationResult {
  messageId: string;
  body: string;
  /** The language the provider found the message to be in. */
  sourceLanguage: string;
  /** Already in the target language, so shown as it is. */
  skipped: boolean;
}

/** What the server answered for a batch of messages. */
export interface TranslateResponse {
  provider: TranslationProvider;
  targetLanguage: string;
  translations: TranslationResult[];
  /** Messages it would not translate, without saying why. */
  missing: string[];
}
