import { computed } from "vue";
import { createGlobalState, useLocalStorage } from "@vueuse/core";

/**
 * How an incoming call sounds, on this device.
 *
 * Kept on the device rather than the account, on purpose: the laptop at work
 * and the phone at home rarely want the same ring. Kept per server too, because
 * the desktop client holds several servers in one place and a community's
 * calls and a company's may want different rings.
 *
 * The built-in tones are made with Web Audio rather than shipped as files, so
 * they cost nothing to download. A person's own tone is an audio file they
 * choose, kept in this browser up to a modest size.
 */

export interface Tone {
  id: string;
  name: string;
  /** For a tone somebody added: the file, as a data URL. */
  source?: string;
}

/** The tones made here, by note and timing. */
const builtIn: Record<string, { name: string; notes: [number, number, number][] }> = {
  // [offset seconds, frequency, length seconds]
  classic: {
    name: "Classic",
    notes: [
      [0, 880, 0.22],
      [0.25, 660, 0.22],
    ],
  },
  soft: { name: "Soft", notes: [[0, 523, 0.5]] },
  bright: {
    name: "Bright",
    notes: [
      [0, 784, 0.12],
      [0.15, 988, 0.12],
      [0.3, 1175, 0.18],
    ],
  },
  pulse: {
    name: "Pulse",
    notes: [
      [0, 698, 0.08],
      [0.12, 698, 0.08],
      [0.24, 698, 0.08],
    ],
  },
  low: {
    name: "Low",
    notes: [
      [0, 330, 0.3],
      [0.35, 392, 0.3],
    ],
  },
};

/** The largest tone a person may add: a ringtone, not a song. */
export const maxToneBytes = 400 * 1024;

interface Settings {
  muted: boolean;
  /** Whether to vibrate while ringing, where the device can. Separate from the
   *  sound, as on a phone: muted and vibrating is the usual "silent". */
  vibrate: boolean;
  tone: string;
  /** A tone for one person, by their address. */
  byPerson: Record<string, string>;
  custom: Tone[];
}

const defaults = (): Settings => ({
  muted: false,
  vibrate: true,
  tone: "classic",
  byPerson: {},
  custom: [],
});

/** Whether this device can vibrate at all: phones, in browsers that allow it. */
export function canVibrate(): boolean {
  return typeof navigator !== "undefined" && typeof navigator.vibrate === "function";
}

export const useRingtones = createGlobalState(() => {
  const { info } = useServerInfo();
  const serverKey = computed(() => info.value?.canonicalAddress || info.value?.name || "server");
  const stored = useLocalStorage<Record<string, Settings>>("tdc.ringtones", {});

  const settings = computed<Settings>(() => ({ ...defaults(), ...stored.value[serverKey.value] }));
  function update(change: Partial<Settings>) {
    stored.value = { ...stored.value, [serverKey.value]: { ...settings.value, ...change } };
  }

  const tones = computed<Tone[]>(() => [
    ...Object.entries(builtIn).map(([id, tone]) => ({ id, name: tone.name })),
    ...settings.value.custom,
  ]);

  /** Which tone rings for somebody: theirs if set, otherwise the default. */
  function toneFor(address: string): string {
    const chosen = settings.value.byPerson[address] ?? settings.value.tone;
    return tones.value.some((tone) => tone.id === chosen) ? chosen : "classic";
  }

  function setPersonTone(address: string, tone: string | null) {
    const byPerson = { ...settings.value.byPerson };
    if (tone) byPerson[address] = tone;
    else delete byPerson[address];
    update({ byPerson });
  }

  /** Adds a tone from an audio file, refusing one too large to keep here. */
  async function addTone(file: File): Promise<"added" | "too-large" | "not-audio"> {
    if (!file.type.startsWith("audio/")) return "not-audio";
    if (file.size > maxToneBytes) return "too-large";
    const source = await new Promise<string>((resolve, reject) => {
      const reader = new FileReader();
      reader.onload = () => resolve(String(reader.result));
      reader.onerror = () => reject(reader.error);
      reader.readAsDataURL(file);
    });
    const tone = { id: `custom-${Date.now()}`, name: file.name.replace(/\.[^.]+$/, ""), source };
    update({ custom: [...settings.value.custom, tone] });
    return "added";
  }

  function removeTone(id: string) {
    const byPerson = Object.fromEntries(
      Object.entries(settings.value.byPerson).filter(([, tone]) => tone !== id),
    );
    update({
      custom: settings.value.custom.filter((tone) => tone.id !== id),
      tone: settings.value.tone === id ? "classic" : settings.value.tone,
      byPerson,
    });
  }

  /**
   * Plays a tone, repeating every two seconds until stopped. Returns the stop.
   * A browser that has not been interacted with yet may stay silent; the
   * incoming window is the ring then.
   */
  function play(id: string, repeat = true): () => void {
    const custom = settings.value.custom.find((tone) => tone.id === id);
    if (custom?.source) {
      const audio = new Audio(custom.source);
      audio.loop = repeat;
      void audio.play().catch(() => {});
      return () => {
        audio.pause();
        audio.src = "";
      };
    }

    const notes = (builtIn[id] ?? builtIn.classic)!.notes;
    let context: AudioContext | null = null;
    try {
      context = new AudioContext();
      void context.resume().catch(() => {});
    } catch {
      return () => {};
    }
    const sound = () => {
      if (!context) return;
      for (const [offset, pitch, length] of notes) {
        const tone = context.createOscillator();
        const level = context.createGain();
        const at = context.currentTime + offset;
        tone.frequency.value = pitch;
        level.gain.setValueAtTime(0.0001, at);
        level.gain.exponentialRampToValueAtTime(0.15, at + 0.02);
        level.gain.exponentialRampToValueAtTime(0.0001, at + length);
        tone.connect(level).connect(context.destination);
        tone.start(at);
        tone.stop(at + length + 0.03);
      }
    };
    sound();
    const timer = repeat ? setInterval(sound, 2000) : null;
    return () => {
      if (timer) clearInterval(timer);
      void context?.close();
      context = null;
    };
  }

  /**
   * Rings for somebody: their tone unless the ringer is muted, and a vibration
   * unless that is off, each where the device can. Returns the stop.
   */
  function ring(address: string): () => void {
    const stopSound = settings.value.muted ? () => {} : play(toneFor(address));
    let timer: ReturnType<typeof setInterval> | null = null;
    if (settings.value.vibrate && canVibrate()) {
      const buzz = () => navigator.vibrate([400, 200, 400]);
      buzz();
      timer = setInterval(buzz, 2000);
    }
    return () => {
      stopSound();
      if (timer) {
        clearInterval(timer);
        navigator.vibrate(0);
      }
    };
  }

  return {
    settings,
    tones,
    ring,
    setVibrate: (vibrate: boolean) => update({ vibrate }),
    toneFor,
    setMuted: (muted: boolean) => update({ muted }),
    setDefault: (tone: string) => update({ tone }),
    setPersonTone,
    addTone,
    removeTone,
    play,
  };
});
