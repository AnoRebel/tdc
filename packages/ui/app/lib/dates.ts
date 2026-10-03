import { tz } from "@date-fns/tz";
import { format, formatDistanceToNowStrict, isSameDay, isSameYear, parseISO } from "date-fns";

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
 * A calendar day the server sent as a day — "2026-10-18" — read as that day.
 *
 * Not an instant, and never put through one: `new Date("2026-10-18")` is
 * midnight UTC, which is the 17th for everybody west of Greenwich. The day's
 * own numbers are formatted as they are, so it reads the same in Honolulu as
 * in Tokyo — the date a project chose is the date everybody sees.
 */
function calendarDay(day: string): Date {
  const [year, month, date] = day.split("-").map(Number);
  // The process's own calendar, used only to carry the three numbers to the
  // formatter, which reads them back in the same calendar.
  return new Date(year ?? 1970, (month ?? 1) - 1, date ?? 1);
}

/** "18 Oct 2026" — a day a project planned by, exactly as it was chosen. */
export function onCalendarDay(day: string): string {
  return format(calendarDay(day), "d MMM yyyy");
}

/** "Sun 18 Oct" — a planned day, with its weekday, where the year goes without saying. */
export function onCalendarWeekday(day: string): string {
  return format(calendarDay(day), "EEE d MMM");
}

/** Today in a zone, as a day: "2026-10-18". What a project's calendar says now. */
export function todayIn(timeZone: string): string {
  return format(new Date(), "yyyy-MM-dd", { in: tz(timeZone) });
}

/** The day so many days after another, both as "YYYY-MM-DD". */
export function daysAfter(day: string, days: number): string {
  const moved = calendarDay(day);
  moved.setDate(moved.getDate() + days);
  return format(moved, "yyyy-MM-dd");
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
