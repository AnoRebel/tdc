import { describe, expect, it } from "vitest";

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
  it("gives only the time for something today", () => {
    const earlier = new Date(Date.now() - 60 * 60 * 1000);
    expect(whenBriefly(earlier, DAR)).toMatch(/^\d{2}:\d{2}$/);
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
