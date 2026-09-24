import { tz } from "@date-fns/tz";
import {
  addDays,
  format,
  formatDistanceToNowStrict,
  isSameDay,
  isSameYear,
  parseISO,
} from "date-fns";

/**
 * When something happened, said the same way everywhere.
 *
 * Through date-fns rather than the browser's own formatter, for two reasons
 * that only show up in a second client. `toLocaleString` asks the runtime for
 * its idea of a locale, and the desktop webview's is not always the browser's —
 * so the same message could be stamped differently in the two clients. And a
 * format written as an options bag is written out again at every call site,
 * which is how a conversation ends up with two ways of writing a time.
 *
 * The zone is explicit. A server records an instant; where it is read decides
 * what o'clock that was, and leaving it implicit means it silently follows
 * whatever the process happens to think — which on a desktop client launched
 * from a tray can differ from the session that started it.
 */

/** The zone times are shown in: this machine's, unless one is given. */
function zone(timeZone?: string) {
  return timeZone ? { in: tz(timeZone) } : undefined;
}

/** An instant from the API, which sends ISO 8601. */
function instant(value: string | number | Date): Date {
  if (value instanceof Date) return value;
  if (typeof value === "number") return new Date(value);
  return parseISO(value);
}

/** "14:32" — the time alone, for a message in a conversation. */
export function atTime(value: string | number | Date, timeZone?: string): string {
  return format(instant(value), "HH:mm", zone(timeZone));
}

/** "Monday, 7 September" — the separator between days in a conversation. */
export function onDay(value: string | number | Date, timeZone?: string): string {
  return format(instant(value), "EEEE, d MMMM", zone(timeZone));
}

/** "7 Sep 14:32" — a day and time together, where both matter. */
export function onDayAtTime(value: string | number | Date, timeZone?: string): string {
  return format(instant(value), "d MMM HH:mm", zone(timeZone));
}

/** "7 Sep 2026" — a date where the year is worth stating. */
export function onDate(value: string | number | Date, timeZone?: string): string {
  return format(instant(value), "d MMM yyyy", zone(timeZone));
}

/** "7 Sep 2026, 14:32" — everything, for a record somebody may quote. */
export function fully(value: string | number | Date, timeZone?: string): string {
  return format(instant(value), "d MMM yyyy, HH:mm", zone(timeZone));
}

/** "Tue 14:32" — a weekday and a time, for something being scheduled. */
export function onWeekdayAtTime(value: string | number | Date, timeZone?: string): string {
  return format(instant(value), "EEE HH:mm", zone(timeZone));
}

/**
 * "3 minutes" — how long ago, without the "ago".
 *
 * Strict, so a minute and a half is "2 minutes" rather than "about 2 minutes":
 * a conversation wants the number, and the hedging reads as uncertainty about
 * something the client knows exactly.
 */
export function howLongAgo(value: string | number | Date): string {
  return formatDistanceToNowStrict(instant(value));
}

/**
 * The shortest thing that still says when.
 *
 * Today gives the time, this year the day and month, and anything older the
 * year as well — so a timestamp carries only what the reader cannot infer.
 */
export function whenBriefly(value: string | number | Date, timeZone?: string): string {
  const at = instant(value);
  const now = new Date();

  if (isSameDay(at, now)) return atTime(at, timeZone);
  if (isSameYear(at, now)) return onDayAtTime(at, timeZone);
  return fully(at, timeZone);
}

/**
 * The instant a number of days from now, as RFC 3339 in UTC.
 *
 * For a server that wants an absolute instant — an availability that lapses,
 * say — rather than a wall-clock date that means something different in each
 * time zone it is read in.
 */
export function daysFromNow(days: number): string {
  return addDays(new Date(), days).toISOString();
}
