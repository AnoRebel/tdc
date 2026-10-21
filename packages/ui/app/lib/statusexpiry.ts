import { differenceInSeconds, endOfDay, endOfWeek, isSameDay } from "date-fns";

/**
 * When a status clears itself.
 *
 * The server takes a number of seconds from now; people think in "for an
 * hour" and "until the end of today". This turns the second into the first,
 * and says how long is left in words a person reads at a glance.
 */

export type ExpiryChoice = "30m" | "1h" | "4h" | "today" | "week" | "never";

export const expiryChoices: ExpiryChoice[] = ["never", "30m", "1h", "4h", "today", "week"];

/**
 * Seconds from now until a choice lapses, or undefined for never.
 *
 * "Today" ends at midnight where the person is, and "this week" at the end of
 * Sunday — the working week, rather than whatever week the locale starts on.
 * Never less than a minute: a status chosen at 23:59:50 to last "today"
 * should not vanish before the menu has closed.
 */
export function secondsUntil(choice: ExpiryChoice, now: Date = new Date()): number | undefined {
  switch (choice) {
    case "never":
      return undefined;
    case "30m":
      return 30 * 60;
    case "1h":
      return 60 * 60;
    case "4h":
      return 4 * 60 * 60;
    case "today":
      return Math.max(60, differenceInSeconds(endOfDay(now), now) + 1);
    case "week":
      return Math.max(60, differenceInSeconds(endOfWeek(now, { weekStartsOn: 1 }), now) + 1);
  }
}

/** Seconds from now until a chosen moment, or undefined if it has passed. */
export function secondsUntilMoment(moment: Date, now: Date = new Date()): number | undefined {
  const seconds = differenceInSeconds(moment, now);
  return seconds > 0 ? seconds : undefined;
}

/** How long a status has left, as a number and its unit, for a translation to word. */
export interface Remaining {
  unit: "minutes" | "hours" | "days";
  count: number;
}

/**
 * What is left of a status, rounded the way somebody would say it.
 *
 * Up rather than down for minutes — "1 minute left" when there are forty
 * seconds, never "0 minutes" — and down for hours and days, so "2 hours" is
 * never said of one hour and a half. Undefined once it has lapsed.
 */
export function remaining(expiresAt: Date, now: Date = new Date()): Remaining | undefined {
  const seconds = differenceInSeconds(expiresAt, now);
  if (seconds <= 0) return undefined;

  if (seconds < 60 * 60) return { unit: "minutes", count: Math.ceil(seconds / 60) };
  if (seconds < 24 * 60 * 60) return { unit: "hours", count: Math.floor(seconds / 3600) };
  return { unit: "days", count: Math.floor(seconds / 86_400) };
}

/** Whether a status lapses later today, so "until 15:00" needs no day. */
export function lapsesToday(expiresAt: Date, now: Date = new Date()): boolean {
  return isSameDay(expiresAt, now);
}
