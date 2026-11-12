import { describe, expect, it } from "vitest";

import {
  buildRule,
  byDay,
  describeWhen,
  fromLocalInput,
  monthGrid,
  noRepeat,
  parseRule,
  toLocalInput,
  weekOf,
} from "./events";

describe("event times", () => {
  it("reads a wall-clock time in the event's zone, whatever the process's", () => {
    expect(fromLocalInput("2026-11-07T18:00", "Africa/Dar_es_Salaam")).toBe(
      "2026-11-07T15:00:00.000Z",
    );
    expect(fromLocalInput("2026-07-07T19:00", "Europe/London")).toBe("2026-07-07T18:00:00.000Z");
    expect(fromLocalInput("2026-12-07T19:00", "Europe/London")).toBe("2026-12-07T19:00:00.000Z");
    expect(toLocalInput("2026-11-07T15:00:00Z", "Africa/Dar_es_Salaam")).toBe("2026-11-07T18:00");
  });

  it("shows the organiser's time only when the offset differs", () => {
    const occurrence = { startsAt: "2026-11-07T15:00:00Z", endsAt: "2026-11-07T17:00:00Z" };
    const event = { timeZone: "Africa/Dar_es_Salaam", allDay: false };
    const london = describeWhen(occurrence, event, "Europe/London");
    expect(london.time).toBe("15:00 – 17:00");
    expect(london.theirs).toMatch(/^Sat 18:00 /);
    // Nairobi and Dar es Salaam share an offset: nothing to add.
    expect(describeWhen(occurrence, event, "Africa/Nairobi").theirs).toBeUndefined();
  });

  it("keeps whole days on their own dates for every reader", () => {
    const occurrence = { startsAt: "2026-11-30T21:00:00Z", endsAt: "2026-12-02T21:00:00Z" };
    const when = describeWhen(
      occurrence,
      { timeZone: "Africa/Dar_es_Salaam", allDay: true },
      "Pacific/Honolulu",
    );
    expect(when.day).toBe("Tue 1 Dec 2026 – Wed 2 Dec 2026");
    expect(when.time).toBe("");
  });

  it("groups occurrences by the reader's day", () => {
    const late = { startsAt: "2026-11-07T22:30:00Z" };
    expect([...byDay([late], "Africa/Dar_es_Salaam").keys()]).toEqual(["2026-11-08"]);
    expect([...byDay([late], "Europe/London").keys()]).toEqual(["2026-11-07"]);
  });
});

describe("calendar grids", () => {
  it("fills whole weeks, Monday first", () => {
    const days = monthGrid("2026-11");
    expect(days[0]).toBe("2026-10-26");
    expect(days.length % 7).toBe(0);
    expect(days.at(-1)).toBe("2026-12-06");
    expect(weekOf("2026-11-07")).toEqual([
      "2026-11-02",
      "2026-11-03",
      "2026-11-04",
      "2026-11-05",
      "2026-11-06",
      "2026-11-07",
      "2026-11-08",
    ]);
  });
});

describe("repeats", () => {
  it("writes the subset the server accepts, and reads it back", () => {
    const weekly = {
      ...noRepeat(),
      frequency: "weekly" as const,
      days: ["TU" as const, "TH" as const],
      end: "count" as const,
      count: 6,
    };
    expect(buildRule(weekly, "2026-11-03T19:00", "Europe/London")).toBe(
      "FREQ=WEEKLY;COUNT=6;BYDAY=TU,TH",
    );
    expect(parseRule("FREQ=WEEKLY;COUNT=6;BYDAY=TU,TH", "Europe/London")).toMatchObject({
      frequency: "weekly",
      count: 6,
      end: "count",
      days: ["TU", "TH"],
    });

    const monthly = { ...noRepeat(), frequency: "monthly" as const, monthly: "weekday" as const };
    expect(buildRule(monthly, "2026-11-10T18:00", "Africa/Dar_es_Salaam")).toBe(
      "FREQ=MONTHLY;BYDAY=2TU",
    );
    expect(buildRule(monthly, "2026-11-24T18:00", "Africa/Dar_es_Salaam")).toBe(
      "FREQ=MONTHLY;BYDAY=-1TU",
    );

    const until = {
      ...noRepeat(),
      frequency: "daily" as const,
      end: "until" as const,
      until: "2026-12-31",
    };
    expect(buildRule(until, "2026-11-01T09:00", "Africa/Dar_es_Salaam")).toBe(
      "FREQ=DAILY;UNTIL=20261231T205900Z",
    );
    expect(parseRule("FREQ=DAILY;UNTIL=20261231T205900Z", "Africa/Dar_es_Salaam").until).toBe(
      "2026-12-31",
    );

    expect(buildRule(noRepeat(), "2026-11-01T09:00", "UTC")).toBe("");
    expect(parseRule("FREQ=YEARLY", "UTC").frequency).toBe("none");
  });
});
