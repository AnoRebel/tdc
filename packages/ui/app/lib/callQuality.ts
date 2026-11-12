/**
 * How much a call sends when the connection cannot carry it all.
 *
 * Video gives way first and sound last, and the call itself never: a call
 * that has dropped to sound alone on a train is still a call, and one that
 * ended because the picture stuttered is not. Each step down is taken only
 * after the connection has been short twice running, and each step back up
 * only after it has been comfortable five times running, so a moment's
 * congestion does not set the picture flickering between qualities.
 */

/** 0 everything; 1 lighter video; 2 low video; 3 no camera; 4 lighter sound too. */
export type QualityLevel = 0 | 1 | 2 | 3 | 4;

export interface QualityState {
  level: QualityLevel;
  /** Samples running that asked for less, and for more. */
  short: number;
  comfortable: number;
}

/** One reading of the connection: what it can send, and whether the browser says bandwidth is what holds video back. */
export interface QualitySample {
  kbps: number | null;
  limited: boolean;
  /**
   * Whether any video is going out. The browser's estimate only grows with
   * what is asked of it, so with the camera off it stays low however good
   * the connection is, and means nothing.
   */
  video?: boolean;
}

export const initialQuality: QualityState = { level: 0, short: 0, comfortable: 0 };

/** The least each level needs to be worth keeping, in kilobits a second. */
const needs = [800, 400, 200, 60] as const;

function wanted(sample: QualitySample, level: QualityLevel): QualityLevel {
  // Below the camera step the missing video is the person's own choice, not
  // ours, so a low estimate says nothing about the connection.
  if (sample.video === false && level < 3) sample = { ...sample, kbps: null };
  if (sample.kbps === null)
    return (sample.limited ? Math.min(4, level + 1) : level) as QualityLevel;
  const fits = needs.findIndex((need) => sample.kbps! >= need);
  const target = (fits === -1 ? 4 : fits) as QualityLevel;
  // Told it is short while the estimate says otherwise: believe the shortage.
  return sample.limited && target <= level ? (Math.min(4, level + 1) as QualityLevel) : target;
}

/** The next state, after one more reading. */
export function stepQuality(state: QualityState, sample: QualitySample): QualityState {
  const target = wanted(sample, state.level);
  if (target > state.level) {
    const short = state.short + 1;
    return short >= 2
      ? { level: (state.level + 1) as QualityLevel, short: 0, comfortable: 0 }
      : { ...state, short, comfortable: 0 };
  }
  if (target < state.level) {
    const comfortable = state.comfortable + 1;
    return comfortable >= 5
      ? { level: (state.level - 1) as QualityLevel, short: 0, comfortable: 0 }
      : { ...state, short: 0, comfortable };
  }
  return { ...state, short: 0, comfortable: 0 };
}

/** What each level asks of the camera, the shared screen and the microphone. */
export function encodingFor(
  level: QualityLevel,
  slot: "audio" | "camera" | "screen",
): Partial<RTCRtpEncodingParameters> {
  if (slot === "audio") return level >= 4 ? { maxBitrate: 24_000 } : { maxBitrate: undefined };
  if (slot === "screen") {
    if (level >= 3) return { maxBitrate: 200_000, maxFramerate: 5 };
    if (level >= 2) return { maxBitrate: 400_000, maxFramerate: 8 };
    return { maxBitrate: undefined, maxFramerate: undefined };
  }
  if (level >= 2) return { scaleResolutionDownBy: 4, maxFramerate: 10, maxBitrate: 150_000 };
  if (level >= 1) return { scaleResolutionDownBy: 2, maxFramerate: 15, maxBitrate: undefined };
  return { scaleResolutionDownBy: 1, maxFramerate: undefined, maxBitrate: undefined };
}
