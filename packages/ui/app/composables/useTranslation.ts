import {
  isApiError,
  type TranslateResponse,
  type TranslationConnection,
  type TranslationOverview,
  type TranslationPreferences,
  type TranslationProvider,
} from "@tdc/types";

/**
 * Translating messages inline, with whichever provider this person chose.
 *
 * Holds what the server says this person may use and has chosen, decides for
 * each message whether it is shown translated, and fetches translations in
 * batches. Everything is shared at module scope, like the server description:
 * a conversation renders a TranslationBody per message, and a hundred of them
 * must make one request for the settings and one per batch of messages, not a
 * hundred.
 *
 * The provider code itself — the on-device translator and the segmenter — is
 * imported on first use, so somebody who never translates downloads none of it.
 */

/** Why a translation is not shown, in terms the reader can act on. */
export type TranslationFailure =
  "needs-click" | "rate-limited" | "not-connected" | "unsupported" | "failed";

export interface TranslationEntry {
  state: "pending" | "done" | "failed";
  body?: string;
  sourceLanguage?: string;
  /** Already in the reader's language, so shown as it is. */
  skipped?: boolean;
  failure?: TranslationFailure;
}

const overview = ref<TranslationOverview | null>(null);
let loading: Promise<void> | null = null;

/** A reader's choice for one message: shown translated, or as written. */
const chosen = reactive(new Map<string, boolean>());
/** Translations this session, by provider, language, message and body. */
const entries = reactive(new Map<string, TranslationEntry>());

/** Messages waiting to be asked about, per room, so a screenful is one request. */
const queued = new Map<string, Map<string, string>>();
let flushTimer: ReturnType<typeof setTimeout> | undefined;

/** The most messages the server takes in one request. */
const BATCH = 50;

/*
 * Set when the browser has the on-device APIs but they do not work: the globals
 * exist and the models do not — a Chromium build without them, hardware Chrome
 * declines to run them on. Translating then falls through to the next provider
 * rather than failing on every message.
 */
const deviceBroken = ref(false);

/** Whether the browser has the on-device APIs. Checked without loading anything. */
const deviceSupported = () =>
  !deviceBroken.value && "Translator" in globalThis && "LanguageDetector" in globalThis;

export function useTranslation() {
  const api = useApi();
  const { offers } = useServerInfo();
  const { $getLocale } = useI18n();

  const offered = computed(() => offers("translation"));

  async function load(force = false) {
    if (!offered.value) return;
    if (!force && (overview.value || loading)) return loading ?? undefined;
    loading = api
      .get<TranslationOverview>("/api/me/translation")
      .then((loaded) => {
        overview.value = loaded;
      })
      .catch(() => {
        // Signed out, or the server unreachable: translation is simply not
        // offered, and every message shows as written.
      })
      .finally(() => {
        loading = null;
      });
    return loading;
  }

  // The capability arrives with the server description, which may be later.
  watch(offered, (on) => on && void load(), { immediate: true });

  function state(name: TranslationProvider) {
    return overview.value?.providers.find((provider) => provider.name === name);
  }

  /** Whether a provider would work for this person, here, now. */
  function usable(name: TranslationProvider): boolean {
    const provider = state(name);
    if (!provider?.allowed) return false;
    if (name === "device") return deviceSupported();
    if (name === "libretranslate") return provider.shared || provider.connected;
    return provider.connected;
  }

  /*
   * The provider in use: the person's choice where it works, otherwise the
   * free default — on this device where the browser can, the operator's
   * LibreTranslate where there is one — and otherwise whatever they have
   * connected. Never a commercial provider nobody connected.
   */
  const automaticProvider = computed<TranslationProvider | "">(() => {
    for (const name of ["device", "libretranslate", "google", "microsoft", "deepl"] as const) {
      if (usable(name)) return name;
    }
    return "";
  });

  const provider = computed<TranslationProvider | "">(() => {
    const preferred = overview.value?.preferences.provider;
    return preferred && usable(preferred) ? preferred : automaticProvider.value;
  });

  /** The language messages are translated into: chosen, or the interface's. */
  const target = computed(() => overview.value?.preferences.targetLanguage || $getLocale() || "en");

  const available = computed(() => offered.value && provider.value !== "");

  /** Whether a conversation is translated automatically for this person. */
  function autoFor(roomId: string): boolean {
    const rule = overview.value?.rooms.find((room) => room.roomId === roomId);
    return rule ? rule.autoTranslate : (overview.value?.preferences.autoTranslate ?? false);
  }

  /**
   * Whether a message is shown translated.
   *
   * The reader's choice for that message first, then the conversation's, then
   * the server-wide one — never automatically for what the reader wrote, which
   * they already understand.
   */
  function showsTranslation(messageId: string, roomId: string, mine: boolean): boolean {
    if (!available.value) return false;
    return chosen.get(messageId) ?? (!mine && autoFor(roomId));
  }

  function keyFor(messageId: string, body: string) {
    return `${provider.value}\u0000${target.value}\u0000${messageId}\u0000${body}`;
  }

  /** What is known about a message's translation into the current language. */
  function entry(messageId: string, body: string): TranslationEntry | undefined {
    return entries.get(keyFor(messageId, body));
  }

  /** Asks for a message's translation, once; later calls are free. */
  function request(roomId: string, messageId: string, body: string) {
    if (!available.value) return;
    const key = keyFor(messageId, body);
    if (entries.has(key)) return;
    entries.set(key, { state: "pending" });

    let room = queued.get(roomId);
    if (!room) queued.set(roomId, (room = new Map()));
    room.set(messageId, body);

    // A short wait gathers a whole screen of messages into one request.
    flushTimer ??= setTimeout(() => {
      flushTimer = undefined;
      void flush();
    }, 40);
  }

  async function flush() {
    const using = provider.value;
    const language = target.value;
    const batches = [...queued.entries()];
    queued.clear();
    if (!using) return;

    for (const [roomId, messages] of batches) {
      const ids = [...messages.keys()];
      for (let start = 0; start < ids.length; start += BATCH) {
        const slice = ids.slice(start, start + BATCH).map((id) => [id, messages.get(id)!] as const);
        if (using === "device") await viaDevice(roomId, slice, language);
        else await viaServer(roomId, slice, using, language);
      }
    }
  }

  function settle(
    id: string,
    body: string,
    using: string,
    language: string,
    value: TranslationEntry,
  ) {
    entries.set(`${using}\u0000${language}\u0000${id}\u0000${body}`, value);
  }

  async function viaDevice(
    roomId: string,
    messages: (readonly [string, string])[],
    language: string,
  ) {
    const device = await import("#layers/ui/app/lib/translation-device");
    const requeue: [string, string][] = [];
    for (const [id, body] of messages) {
      try {
        const done = await device.translateOnDevice(body, language);
        settle(id, body, "device", language, { state: "done", ...done });
      } catch (cause) {
        if (
          !(cause instanceof device.NeedsActivationError) &&
          !(cause instanceof device.UnsupportedPairError) &&
          !overview.value?.preferences.provider
        ) {
          /*
           * The device cannot translate at all, and nobody chose it: stop
           * using it, and ask again of whatever the default is now.
           */
          deviceBroken.value = true;
          entries.delete(`device\u0000${language}\u0000${id}\u0000${body}`);
          requeue.push([id, body]);
          continue;
        }
        const failure: TranslationFailure =
          cause instanceof device.NeedsActivationError
            ? "needs-click"
            : cause instanceof device.UnsupportedPairError
              ? "unsupported"
              : "failed";
        settle(id, body, "device", language, { state: "failed", failure });
      }
    }
    for (const [id, body] of requeue) request(roomId, id, body);
  }

  async function viaServer(
    roomId: string,
    messages: (readonly [string, string])[],
    using: TranslationProvider,
    language: string,
  ) {
    /*
     * A message the browser can tell is already in the reader's language is
     * not sent at all: it would cost a request and, with a commercial
     * provider, the reader's money, to be told what the detector already knew.
     * Only where an on-device detector exists; nothing is downloaded for it.
     */
    let pending = messages;
    if (deviceSupported()) {
      const device = await import("#layers/ui/app/lib/translation-device");
      const kept: (readonly [string, string])[] = [];
      for (const message of messages) {
        const found = await device.detectLanguage(message[1]);
        if (found && found.split("-")[0] === language.split("-")[0]) {
          settle(message[0], message[1], using, language, {
            state: "done",
            body: message[1],
            sourceLanguage: found,
            skipped: true,
          });
        } else kept.push(message);
      }
      pending = kept;
    }
    if (!pending.length) return;

    try {
      const answer = await api.post<TranslateResponse>(
        `/api/rooms/${encodeURIComponent(roomId)}/translations`,
        { messages: pending.map(([id]) => id), targetLanguage: language, provider: using },
      );
      const byId = new Map(answer.translations.map((result) => [result.messageId, result]));
      for (const [id, body] of pending) {
        const result = byId.get(id);
        settle(
          id,
          body,
          using,
          language,
          result
            ? {
                state: "done",
                body: result.body,
                sourceLanguage: result.sourceLanguage,
                skipped: result.skipped,
              }
            : { state: "failed", failure: "unsupported" },
        );
      }
    } catch (cause) {
      const failure: TranslationFailure = !isApiError(cause)
        ? "failed"
        : cause.code === "rate-limited"
          ? "rate-limited"
          : cause.code === "conflict"
            ? "not-connected"
            : cause.code === "invalid"
              ? "unsupported"
              : "failed";
      for (const [id, body] of pending)
        settle(id, body, using, language, { state: "failed", failure });
    }
  }

  /**
   * Shows a message translated or as written, from its own button.
   *
   * Turning it on asks again where the last attempt failed: the press may be
   * exactly what was missing, since a browser downloads a language model only
   * in answer to one.
   */
  function toggle(roomId: string, messageId: string, body: string, mine: boolean) {
    const showing = showsTranslation(messageId, roomId, mine);
    chosen.set(messageId, !showing);
    if (!showing) {
      const key = keyFor(messageId, body);
      if (entries.get(key)?.state === "failed") entries.delete(key);
      request(roomId, messageId, body);
    }
  }

  /** Asks again after a failure, from the reader's own press. */
  function retry(roomId: string, messageId: string, body: string) {
    entries.delete(keyFor(messageId, body));
    request(roomId, messageId, body);
  }

  /** Whether the reader chose, for this message, rather than a rule deciding. */
  function chosenFor(messageId: string): boolean | undefined {
    return chosen.get(messageId);
  }

  /** Clears earlier choices and failures, after a setting changes what applies. */
  function forget() {
    entries.forEach((value, key) => value.state === "failed" && entries.delete(key));
  }

  async function setPreferences(preferences: TranslationPreferences) {
    await api.put("/api/me/translation/preferences", preferences);
    if (overview.value) overview.value = { ...overview.value, preferences };
    forget();
  }

  /** Translates one conversation automatically, or not, or as the server-wide choice. */
  async function setRoomAuto(roomId: string, autoTranslate: boolean | null) {
    if (autoTranslate === null)
      await api.delete(`/api/me/translation/rooms/${encodeURIComponent(roomId)}`);
    else
      await api.put(`/api/me/translation/rooms/${encodeURIComponent(roomId)}`, { autoTranslate });
    if (!overview.value) return;
    const rooms = overview.value.rooms.filter((room) => room.roomId !== roomId);
    if (autoTranslate !== null) rooms.push({ roomId, autoTranslate });
    overview.value = { ...overview.value, rooms };
    // The conversation's rule now decides; earlier per-message choices there
    // would otherwise keep overriding it.
    chosen.clear();
  }

  async function connect(name: TranslationProvider, connection: TranslationConnection) {
    await api.put(`/api/me/translation/connections/${name}`, connection);
    await load(true);
    forget();
  }

  async function disconnect(name: TranslationProvider) {
    await api.delete(`/api/me/translation/connections/${name}`);
    await load(true);
  }

  return {
    overview,
    offered,
    available,
    provider,
    automaticProvider,
    target,
    deviceSupported,
    usable,
    autoFor,
    showsTranslation,
    entry,
    request,
    toggle,
    retry,
    chosenFor,
    load,
    setPreferences,
    setRoomAuto,
    connect,
    disconnect,
  };
}
