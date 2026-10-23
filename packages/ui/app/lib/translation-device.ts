import { replace, split, translatable } from "#layers/ui/app/lib/translation-segments";

/*
 * Translation on the reader's own device.
 *
 * The browser's Translator and Language Detector APIs, where it has them:
 * Chrome on the desktop today, not Firefox, Safari or the webviews the desktop
 * application runs in on macOS and Linux. Free, and private in the way nothing
 * else here is — the model runs locally, so the message never leaves the
 * machine. The browser downloads a model once per language pair, from its
 * vendor, which tells the vendor a language and nothing about the message.
 *
 * Loaded only when somebody first translates, so nobody who never does pays for
 * this file.
 */

type Availability = "unavailable" | "downloadable" | "downloading" | "available";

interface CreateMonitor extends EventTarget {}

interface TranslatorInstance {
  translate(input: string): Promise<string>;
}

interface DetectorInstance {
  detect(input: string): Promise<{ detectedLanguage: string; confidence: number }[]>;
}

interface TranslatorGlobal {
  availability(options: { sourceLanguage: string; targetLanguage: string }): Promise<Availability>;
  create(options: {
    sourceLanguage: string;
    targetLanguage: string;
    monitor?: (monitor: CreateMonitor) => void;
  }): Promise<TranslatorInstance>;
}

interface DetectorGlobal {
  availability(options?: object): Promise<Availability>;
  create(options?: { monitor?: (monitor: CreateMonitor) => void }): Promise<DetectorInstance>;
}

const scope = globalThis as unknown as {
  Translator?: TranslatorGlobal;
  LanguageDetector?: DetectorGlobal;
};

/** Whether this browser has both halves. Translating needs a source language. */
export function deviceSupported(): boolean {
  return Boolean(scope.Translator && scope.LanguageDetector);
}

/**
 * A model has to be downloaded, and the browser allows that only in answer to
 * a click. Distinct from other failures because the remedy is different: the
 * message's own Translate button is that click.
 */
export class NeedsActivationError extends Error {
  constructor() {
    super("needs a click to download the language model");
    this.name = "NeedsActivationError";
  }
}

/** The pair cannot be translated here at all. */
export class UnsupportedPairError extends Error {
  constructor() {
    super("this browser cannot translate between these languages");
    this.name = "UnsupportedPairError";
  }
}

const primary = (language: string) => language.toLowerCase().split("-")[0] ?? "";

function activationRefused(cause: unknown): boolean {
  return cause instanceof DOMException && cause.name === "NotAllowedError";
}

let detector: Promise<DetectorInstance> | null = null;
const translators = new Map<string, Promise<TranslatorInstance>>();

function detectorInstance(): Promise<DetectorInstance> {
  detector ??= scope.LanguageDetector!.create().catch((cause: unknown) => {
    detector = null;
    throw activationRefused(cause) ? new NeedsActivationError() : cause;
  });
  return detector;
}

/**
 * The language a text is in, or "" where the detector is not sure enough to
 * act on. Only an available model is used: detection is an optimisation, and
 * never worth a download on its own.
 */
export async function detectLanguage(text: string): Promise<string> {
  if (!scope.LanguageDetector) return "";
  try {
    if (!detector && (await scope.LanguageDetector.availability()) !== "available") return "";
    const [best] = await (await detectorInstance()).detect(text);
    return best && best.detectedLanguage !== "und" && best.confidence >= 0.7
      ? best.detectedLanguage
      : "";
  } catch {
    return "";
  }
}

async function translatorFor(source: string, target: string): Promise<TranslatorInstance> {
  const key = `${source}>${target}`;
  let made = translators.get(key);
  if (!made) {
    made = (async () => {
      const languages = { sourceLanguage: source, targetLanguage: target };
      if ((await scope.Translator!.availability(languages)) === "unavailable") {
        throw new UnsupportedPairError();
      }
      try {
        return await scope.Translator!.create(languages);
      } catch (cause) {
        throw activationRefused(cause) ? new NeedsActivationError() : cause;
      }
    })();
    // A failure is not remembered: the next click may succeed where this did not.
    made.catch(() => translators.delete(key));
    translators.set(key, made);
  }
  return made;
}

export interface DeviceTranslation {
  body: string;
  sourceLanguage: string;
  skipped: boolean;
}

/** Translates one message body, keeping code, mentions and links as written. */
export async function translateOnDevice(body: string, target: string): Promise<DeviceTranslation> {
  if (!deviceSupported()) throw new UnsupportedPairError();

  const segments = split(body);
  const texts = translatable(segments);
  if (!texts.length) return { body, sourceLanguage: "", skipped: true };

  // Detection may need its own model; a refusal here needs the same click.
  const [best] = await (await detectorInstance()).detect(texts.join("\n"));
  const source = best && best.detectedLanguage !== "und" ? best.detectedLanguage : "";
  if (!source) throw new UnsupportedPairError();
  if (primary(source) === primary(target)) return { body, sourceLanguage: source, skipped: true };

  const translator = await translatorFor(primary(source), primary(target));
  const translated = await Promise.all(texts.map((text) => translator.translate(text)));
  return { body: replace(segments, translated), sourceLanguage: source, skipped: false };
}
