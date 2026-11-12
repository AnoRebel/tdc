import { TZDate, tz } from "@date-fns/tz";
import {
  addDays,
  addMonths,
  endOfMonth,
  format,
  isSameDay,
  parseISO,
  startOfMonth,
  startOfWeek,
} from "date-fns";
import type { CalendarEvent } from "@tdc/types";

/**
 * Times, repeats and calendars for events.
 *
 * The server keeps instants in UTC and the zone the organiser meant beside
 * them. Everything here turns those into what a person reads — in their own
 * zone, and in the event's when the two differ — and back, without ever
 * asking the process what zone it is in: a form filled in Dar es Salaam for an
 * event in London means London's 19:00, whatever the machine thinks.
 */

/** The zone this browser or webview is in, by IANA name. */
export function viewerZone(): string {
  return Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";
}

/** Every zone the runtime knows, for a picker; the viewer's first. */
export function zoneNames(): string[] {
  const supported = (Intl as unknown as { supportedValuesOf?: (key: string) => string[] })
    .supportedValuesOf;
  const names = supported ? supported("timeZone") : ["UTC"];
  const mine = viewerZone();
  return [mine, ...names.filter((name) => name !== mine)];
}

function at(value: string | Date): Date {
  return typeof value === "string" ? parseISO(value) : value;
}

/** What a datetime-local input shows for an instant in a zone. */
export function toLocalInput(value: string | Date, zone: string): string {
  return format(at(value), "yyyy-MM-dd'T'HH:mm", { in: tz(zone) });
}

/** The day a date input shows for an instant in a zone. */
export function toDateInput(value: string | Date, zone: string): string {
  return format(at(value), "yyyy-MM-dd", { in: tz(zone) });
}

/**
 * The instant a wall-clock time means in a zone: "2026-11-07T18:00" in
 * Africa/Dar_es_Salaam is 15:00 UTC. Accepts a bare date as midnight.
 */
export function fromLocalInput(value: string, zone: string): string {
  const [date, time = "00:00"] = value.split("T");
  const [year, month, day] = (date ?? "").split("-").map(Number);
  const [hour, minute] = time.split(":").map(Number);
  const local = new TZDate(year ?? 1970, (month ?? 1) - 1, day ?? 1, hour ?? 0, minute ?? 0, zone);
  return new Date(local.getTime()).toISOString();
}

/** The offset a zone has at an instant, in minutes, for comparing zones. */
function offsetAt(value: Date, zone: string): number {
  return new TZDate(value.getTime(), zone).getTimezoneOffset();
}

/** "EAT", "GMT+3", "BST" — the zone's short name at that instant. */
export function zoneLabel(value: string | Date, zone: string): string {
  return format(at(value), "zzz", { in: tz(zone) });
}

export interface When {
  /** The day, in the reader's zone: "Sat 7 Nov 2026". */
  day: string;
  /** The times, in the reader's zone: "18:00 – 20:00", or empty for all day. */
  time: string;
  /** The same in the organiser's zone, when it differs: "18:00 EAT". */
  theirs?: string;
}

/**
 * When an occurrence is, for somebody in a zone.
 *
 * The organiser's time is added only when the offsets differ at that moment:
 * two names for one offset (Nairobi and Dar es Salaam) are not a difference
 * worth a second line.
 */
export function describeWhen(
  occurrence: { startsAt: string; endsAt: string },
  event: Pick<CalendarEvent, "timeZone" | "allDay">,
  zone: string = viewerZone(),
): When {
  const start = at(occurrence.startsAt);
  const end = at(occurrence.endsAt);
  if (event.allDay) {
    // Whole days belong to the event's zone, never shifted by the reader's.
    const lastDay = addDays(new TZDate(end.getTime(), event.timeZone), -1);
    const first = format(start, "EEE d MMM yyyy", { in: tz(event.timeZone) });
    const last = format(lastDay, "EEE d MMM yyyy", { in: tz(event.timeZone) });
    return { day: first === last ? first : `${first} – ${last}`, time: "" };
  }
  const mine = { in: tz(zone) };
  const sameDay = isSameDay(start, end, mine);
  const when: When = {
    day: format(start, "EEE d MMM yyyy", mine),
    time: sameDay
      ? `${format(start, "HH:mm", mine)} – ${format(end, "HH:mm", mine)}`
      : `${format(start, "HH:mm", mine)} – ${format(end, "EEE d MMM HH:mm", mine)}`,
  };
  if (offsetAt(start, zone) !== offsetAt(start, event.timeZone)) {
    const theirs = { in: tz(event.timeZone) };
    when.theirs = `${format(start, "EEE HH:mm", theirs)} ${zoneLabel(start, event.timeZone)}`;
  }
  return when;
}

/** The calendar day an instant falls on in a zone, as "2026-11-07". */
export function dayKey(value: string | Date, zone: string = viewerZone()): string {
  return format(at(value), "yyyy-MM-dd", { in: tz(zone) });
}

/**
 * The days a month view shows: whole weeks from the Monday on or before the
 * first to the Sunday on or after the last, as "yyyy-MM-dd".
 */
export function monthGrid(month: string): string[] {
  const first = startOfMonth(parseISO(`${month}-01`));
  const start = startOfWeek(first, { weekStartsOn: 1 });
  const last = endOfMonth(first);
  const days: string[] = [];
  for (let day = start; day <= last || days.length % 7 !== 0; day = addDays(day, 1)) {
    days.push(format(day, "yyyy-MM-dd"));
  }
  return days;
}

/** The seven days of the week that holds a day, Monday first. */
export function weekOf(day: string): string[] {
  const start = startOfWeek(parseISO(day), { weekStartsOn: 1 });
  return Array.from({ length: 7 }, (_, index) => format(addDays(start, index), "yyyy-MM-dd"));
}

/** A month moved by some months, as "yyyy-MM". */
export function shiftMonth(month: string, by: number): string {
  return format(addMonths(parseISO(`${month}-01`), by), "yyyy-MM");
}

/** A day moved by some days, as "yyyy-MM-dd". */
export function shiftDay(day: string, by: number): string {
  return format(addDays(parseISO(day), by), "yyyy-MM-dd");
}

/** The instant a day starts in a zone, for asking the server about a range. */
export function startOfDayIn(day: string, zone: string = viewerZone()): string {
  return fromLocalInput(`${day}T00:00`, zone);
}

/** Groups occurrences by the day each starts on, in a zone, keeping order. */
export function byDay<T extends { occurrenceStartsAt?: string; startsAt: string }>(
  occurrences: T[],
  zone: string = viewerZone(),
): Map<string, T[]> {
  const days = new Map<string, T[]>();
  for (const occurrence of occurrences) {
    const key = dayKey(occurrence.occurrenceStartsAt ?? occurrence.startsAt, zone);
    days.set(key, [...(days.get(key) ?? []), occurrence]);
  }
  return days;
}

/* ------------------------------------------------------------------ */
/* Repeats                                                             */
/* ------------------------------------------------------------------ */

export type Frequency = "none" | "daily" | "weekly" | "monthly";
export type Weekday = "MO" | "TU" | "WE" | "TH" | "FR" | "SA" | "SU";
export const weekdays: Weekday[] = ["MO", "TU", "WE", "TH", "FR", "SA", "SU"];

/** A repeat as the form holds it: the subset the server accepts. */
export interface Repeat {
  frequency: Frequency;
  interval: number;
  /** For weekly: which days. Empty means the start's own weekday. */
  days: Weekday[];
  /** For monthly: on the start's date, or on its "nth weekday". */
  monthly: "date" | "weekday";
  end: "never" | "count" | "until";
  count: number;
  /** A day, "yyyy-MM-dd", the last occurrence may fall on. */
  until: string;
}

export function noRepeat(): Repeat {
  return {
    frequency: "none",
    interval: 1,
    days: [],
    monthly: "date",
    end: "never",
    count: 10,
    until: "",
  };
}

function weekdayOf(localStart: string): Weekday {
  // Monday first, as RRULE and ISO number them.
  const index = (parseISO(localStart.slice(0, 10)).getDay() + 6) % 7;
  return weekdays[index] ?? "MO";
}

/** Which of its month's weekdays a day is: 1–4, or -1 for the last. */
function ordinalOf(localStart: string): number {
  const day = parseISO(localStart.slice(0, 10));
  const date = day.getDate();
  if (date + 7 > endOfMonth(day).getDate()) return -1;
  return Math.floor((date - 1) / 7) + 1;
}

/**
 * The RRULE body for a repeat, given the event's start as a wall-clock time
 * in its zone ("2026-11-07T18:00") and the zone, which the end date is
 * resolved in. Empty for an event that happens once.
 */
export function buildRule(repeat: Repeat, localStart: string, zone: string): string {
  if (repeat.frequency === "none") return "";
  const parts = [`FREQ=${repeat.frequency.toUpperCase()}`];
  if (repeat.interval > 1) parts.push(`INTERVAL=${Math.floor(repeat.interval)}`);
  if (repeat.end === "count") parts.push(`COUNT=${Math.max(1, Math.floor(repeat.count))}`);
  if (repeat.end === "until" && repeat.until) {
    // The end of that day where the event is, as an instant in UTC.
    const until = parseISO(fromLocalInput(`${repeat.until}T23:59`, zone));
    parts.push(`UNTIL=${format(until, "yyyyMMdd'T'HHmmss'Z'", { in: tz("UTC") })}`);
  }
  if (repeat.frequency === "weekly" && repeat.days.length) {
    parts.push(`BYDAY=${weekdays.filter((day) => repeat.days.includes(day)).join(",")}`);
  }
  if (repeat.frequency === "monthly" && repeat.monthly === "weekday") {
    parts.push(`BYDAY=${ordinalOf(localStart)}${weekdayOf(localStart)}`);
  }
  return parts.join(";");
}

/** Reads an RRULE body back into the form, where it fits; anything else is "none". */
export function parseRule(rule: string | undefined, zone: string): Repeat {
  const repeat = noRepeat();
  if (!rule) return repeat;
  const fields = new Map(
    rule.split(";").map((part) => {
      const [key, value = ""] = part.split("=");
      return [key ?? "", value] as const;
    }),
  );
  const frequency = (fields.get("FREQ") ?? "").toLowerCase();
  if (frequency !== "daily" && frequency !== "weekly" && frequency !== "monthly") return repeat;
  repeat.frequency = frequency;
  repeat.interval = Number(fields.get("INTERVAL") ?? 1) || 1;
  if (fields.has("COUNT")) {
    repeat.end = "count";
    repeat.count = Number(fields.get("COUNT")) || 1;
  }
  const until = fields.get("UNTIL");
  if (until) {
    repeat.end = "until";
    const instant =
      until.length > 8
        ? `${until.slice(0, 4)}-${until.slice(4, 6)}-${until.slice(6, 8)}T${until.slice(9, 11)}:${until.slice(11, 13)}:00Z`
        : `${until.slice(0, 4)}-${until.slice(4, 6)}-${until.slice(6, 8)}T12:00:00Z`;
    repeat.until = dayKey(instant, zone);
  }
  const byDay = fields.get("BYDAY");
  if (byDay && frequency === "weekly") {
    repeat.days = byDay
      .split(",")
      .filter((day): day is Weekday => weekdays.includes(day as Weekday));
  }
  if (byDay && frequency === "monthly") repeat.monthly = "weekday";
  return repeat;
}

/* ------------------------------------------------------------------ */
/* Other calendars                                                     */
/* ------------------------------------------------------------------ */

function compact(value: string): string {
  return format(parseISO(value), "yyyyMMdd'T'HHmmss'Z'", { in: tz("UTC") });
}

function compactDay(value: string, zone: string): string {
  return format(parseISO(value), "yyyyMMdd", { in: tz(zone) });
}

/**
 * "Add to Google Calendar": opens Google's own form with the event filled in.
 * Nothing is sent to Google by this server; the person's browser goes there
 * because they asked to.
 */
export function googleCalendarLink(
  event: Pick<CalendarEvent, "title" | "timeZone" | "allDay" | "location" | "recurrence">,
  occurrence: { startsAt: string; endsAt: string },
  details: string,
): string {
  const dates = event.allDay
    ? `${compactDay(occurrence.startsAt, event.timeZone)}/${compactDay(occurrence.endsAt, event.timeZone)}`
    : `${compact(occurrence.startsAt)}/${compact(occurrence.endsAt)}`;
  const query = new URLSearchParams({
    action: "TEMPLATE",
    text: event.title,
    dates,
    details,
    ctz: event.timeZone,
  });
  if (event.location) query.set("location", event.location);
  if (event.recurrence) query.set("recur", `RRULE:${event.recurrence}`);
  return `https://calendar.google.com/calendar/render?${query}`;
}

/** "Add to Outlook": Outlook on the web's compose form, filled in. */
export function outlookLink(
  event: Pick<CalendarEvent, "title" | "allDay" | "location">,
  occurrence: { startsAt: string; endsAt: string },
  details: string,
): string {
  const query = new URLSearchParams({
    path: "/calendar/action/compose",
    rru: "addevent",
    subject: event.title,
    startdt: occurrence.startsAt,
    enddt: occurrence.endsAt,
    body: details,
    allday: String(event.allDay),
  });
  if (event.location) query.set("location", event.location);
  return `https://outlook.live.com/calendar/0/deeplink/compose?${query}`;
}

/** A feed's address for a calendar application: webcal, which they open directly. */
export function webcalAddress(httpAddress: string): string {
  return httpAddress.replace(/^https?:\/\//, "webcal://");
}

/** Somebody, or a channel, chosen in a picker. */
export interface Picked {
  id: string;
  name: string;
  kind: "person" | "channel";
}

/*
 * A choice of "no channel", for a Select — which cannot hold an empty value.
 * Neither is a channel's id, which is a UUID.
 */
export const WHOLE_SERVER = "server";
export const ALL_CHANNELS = "all";
