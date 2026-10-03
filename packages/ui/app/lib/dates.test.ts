import { afterEach, describe, expect, it, vi } from "vitest";

import {
  atTime,
  fully,
  howLongAgo,
  onDate,
  onDay,
  onDayAtTime,
  onWeekdayAtTime,
  whenBriefly,
} from "./dates";

/*
 * When something happened, said the same way in both clients.
 *
 * These pin the formats against fixed instants rather than against whatever
 * the runtime's locale happens to be. That is the point of the change: the
 * browser's formatter asks the runtime, and the desktop webview's answer is
 * not always the browser's, so the same message could be stamped two ways.
 *
 * A zone is given explicitly throughout. Without one these would pass in
 * Dar es Salaam and fail in a CI runner set to UTC — which is the same class
 * of bug the library was brought in to remove.
 */

const DAR = "Africa/Dar_es_Salaam"; // UTC+3, and no daylight saving to complicate it.

// A Tuesday, deliberately: the weekday is part of what onDay promises.
const afternoon = "2026-09-08T11:32:00Z"; // 14:32 in Dar es Salaam

/*
 * What the server actually sends, and what the reader must make of it.
 *
 * The server encodes every instant with its offset — "…Z" for UTC or
 * "…+03:00" — so a reader anywhere reconstructs the same moment. These pin
 * that, because the failure mode is silent: a timestamp parsed as local time
 * when it was meant as UTC is not an error, it is a plausible-looking time
 * that is wrong by the offset, and it is wrong by a different amount for
 * every reader.
 */
describe("reading what the server sent", () => {
  it("reconstructs one instant from every way of writing it", () => {
    // The same moment, written four ways a server might legitimately send it.
    const spellings = [
      "2026-09-08T11:32:00Z",
      "2026-09-08T11:32:00.000Z",
      "2026-09-08T14:32:00+03:00",
      "2026-09-08T01:32:00-10:00",
    ];

    // Displayed in one zone, they must all be the same o'clock.
    for (const spelling of spellings) {
      expect(atTime(spelling, DAR), spelling).toBe("14:32");
    }
  });

  it("shows one instant as the local o'clock of wherever it is read", () => {
    // The instant does not move; the clock on the wall does.
    expect(atTime(afternoon, "UTC")).toBe("11:32");
    expect(atTime(afternoon, DAR)).toBe("14:32");
    expect(atTime(afternoon, "Pacific/Honolulu")).toBe("01:32");
    expect(atTime(afternoon, "Asia/Tokyo")).toBe("20:32");
  });

  it("carries a day change across the date line rather than only an hour change", () => {
    // Late evening in Dar es Salaam is already the next morning in Tokyo, and
    // still the previous afternoon in Honolulu. A formatter that shifted the
    // time but not the date would pass an hours-only check and fail here.
    const lateEvening = "2026-09-08T22:30:00+03:00"; // 19:30 UTC

    expect(onDayAtTime(lateEvening, "Asia/Tokyo")).toBe("9 Sep 04:30");
    expect(onDayAtTime(lateEvening, DAR)).toBe("8 Sep 22:30");
    expect(onDayAtTime(lateEvening, "Pacific/Honolulu")).toBe("8 Sep 09:30");
  });

  it("accepts the epoch milliseconds the API sends for a scheduled message", () => {
    // Scheduled sends come back as UnixMilli rather than a string, and a
    // number is unambiguous — but only if it is read as milliseconds. Seconds
    // would land in 1970 and still format without complaint.
    const epochMillis = Date.UTC(2026, 8, 8, 11, 32, 0);

    expect(atTime(epochMillis, DAR)).toBe("14:32");
    expect(onDate(epochMillis, DAR)).toBe("8 Sep 2026");
  });

  it("survives the round trip a scheduled message actually makes", () => {
    // The dialog builds a Date in the reader's zone, sends toISOString(), and
    // the server parses RFC 3339. Whatever the reader's zone, the instant that
    // comes back must be the instant they chose.
    const chosen = new Date("2026-09-08T14:32:00+03:00");
    const sent = chosen.toISOString();

    expect(sent).toBe("2026-09-08T11:32:00.000Z");
    expect(atTime(sent, DAR)).toBe("14:32");
    expect(new Date(sent).getTime()).toBe(chosen.getTime());
  });
});

describe("saying when", () => {
  it("gives the time alone for a message", () => {
    expect(atTime(afternoon, DAR)).toBe("14:32");
  });

  it("names the weekday where a day separates a conversation", () => {
    expect(onDay(afternoon, DAR)).toBe("Tuesday, 8 September");
  });

  it("gives a day and a time where both matter", () => {
    expect(onDayAtTime(afternoon, DAR)).toBe("8 Sep 14:32");
  });

  it("states the year where a date stands alone", () => {
    expect(onDate(afternoon, DAR)).toBe("8 Sep 2026");
  });

  it("gives a weekday and a time for something being scheduled", () => {
    expect(onWeekdayAtTime(afternoon, DAR)).toBe("Tue 14:32");
  });

  it("gives everything for a record somebody may quote", () => {
    expect(fully(afternoon, DAR)).toBe("8 Sep 2026, 14:32");
  });

  /*
   * The zone is the reason this library is here rather than the browser's.
   * The same instant is a different o'clock in two places, and leaving it
   * implicit means it follows whatever the process happens to think.
   */
  it("reads one instant differently in two places, as it should", () => {
    expect(atTime(afternoon, DAR)).toBe("14:32");
    expect(atTime(afternoon, "UTC")).toBe("11:32");
    expect(atTime(afternoon, "America/New_York")).toBe("07:32");
  });

  // The API sends ISO 8601; a Date and a count of milliseconds are accepted too,
  // so a caller never has to convert before asking.
  it("takes an instant however it arrives", () => {
    const asDate = new Date(afternoon);
    expect(atTime(asDate, DAR)).toBe("14:32");
    expect(atTime(asDate.getTime(), DAR)).toBe("14:32");
  });
});

describe("how long ago", () => {
  /*
   * Strict, so a minute and a half is "2 minutes" rather than "about 2
   * minutes". A conversation wants the number; the hedging reads as
   * uncertainty about something the client knows exactly.
   */
  it("does not hedge", () => {
    const ninetySeconds = new Date(Date.now() - 90_000);
    expect(howLongAgo(ninetySeconds)).toBe("2 minutes");
    expect(howLongAgo(ninetySeconds)).not.toMatch(/about/);
  });
});

describe("the shortest thing that still says when", () => {
  /*
   * The clock is fixed: "an hour ago" is yesterday for an hour every night,
   * and a test that reads the real clock fails only in that hour.
   */
  afterEach(() => vi.useRealTimers());

  it("gives only the time for something today", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-09-08T12:00:00+03:00")); // noon in Dar es Salaam

    const earlier = new Date("2026-09-08T11:00:00+03:00");
    expect(whenBriefly(earlier, DAR)).toBe("11:00");
  });

  /*
   * Whether something was today is decided in the zone being shown.
   *
   * 01:30 in Dar es Salaam on the 9th is 22:30 UTC on the 8th. Read at 08:00
   * Dar es Salaam on the 9th, it is earlier today there — and yesterday in
   * UTC, where the machine running this may well be.
   */
  it("decides today in the zone it shows, not the machine's", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-09-09T08:00:00+03:00"));

    const smallHours = new Date("2026-09-09T01:30:00+03:00");
    expect(whenBriefly(smallHours, DAR)).toBe("01:30");
    expect(whenBriefly(smallHours, "UTC")).toBe("8 Sep 22:30");
  });

  it("adds the year only once it is not this one", () => {
    const now = new Date();
    const lastYear = new Date(now.getTime());
    lastYear.setFullYear(now.getFullYear() - 1);

    expect(whenBriefly(lastYear, DAR)).toMatch(String(lastYear.getFullYear()));
  });
});

/*
 * And nothing formats a date any other way.
 *
 * The browser's own formatter asks the runtime for a locale, and the desktop
 * webview's answer is not always the browser's — so a timestamp written that
 * way can read differently in the two clients for the same instant. Every one
 * of them went through this module; this is what keeps them there.
 */
describe("date formatting across both applications", () => {
  it("goes through this module rather than the runtime's own formatter", async () => {
    const { existsSync, readdirSync, readFileSync } = await import("node:fs");
    const { join } = await import("node:path");

    const roots = [
      join(import.meta.dirname, "..", ".."),
      join(import.meta.dirname, "..", "..", "..", "..", "apps", "web", "app"),
      join(import.meta.dirname, "..", "..", "..", "..", "apps", "desktop", "frontend", "app"),
    ];

    const offenders: string[] = [];
    const walk = (directory: string, root: string) => {
      if (!existsSync(directory)) return;
      for (const entry of readdirSync(directory, { withFileTypes: true })) {
        if (entry.name === "node_modules" || entry.name.startsWith(".")) continue;
        const full = join(directory, entry.name);
        if (entry.isDirectory()) {
          walk(full, root);
          continue;
        }
        if (!/\.(ts|vue)$/.test(entry.name) || entry.name.endsWith(".test.ts")) continue;
        if (full.endsWith(join("lib", "dates.ts"))) continue;

        const contents = readFileSync(full, "utf8");
        for (const match of contents.matchAll(
          /\.toLocaleDateString\(|\.toLocaleTimeString\(|new Intl\.DateTimeFormat\(/g,
        )) {
          offenders.push(`${full.slice(root.length + 1)}: ${match[0]}`);
        }
      }
    };
    for (const root of roots) walk(root, root);

    expect(
      offenders.sort(),
      "these ask the runtime for a locale, which the two clients do not always agree on",
    ).toEqual([]);
  });
});
