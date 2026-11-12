import type { Notification } from "@tdc/types";
import { describe, expect, it } from "vitest";

import en from "../../locales/en.json";
import { describeNotification } from "./useNotifications";

/*
 * How a notification reads in the list.
 *
 * Grouped entries for an event were described like a burst of messages, by
 * who sent them — and an event's are mostly sent by the server, so somebody
 * invited to a workshop and then reminded of it read "localhost and 1 other —
 * 2 new", which named neither a person nor the workshop.
 */

function say(key: string, params: Record<string, string | number> = {}): string {
  const template = key
    .split(".")
    .reduce<unknown>((at, part) => (at as Record<string, unknown>)[part], en) as string;
  return template.replace(/\{(\w+)\}/g, (_, name: string) => String(params[name] ?? ""));
}

function notification(overrides: Partial<Notification>): Notification {
  return {
    id: "n1",
    roomId: "r1",
    eventId: "e1",
    reason: "mention",
    sender: "@amina:localhost",
    actor: "@amina:localhost",
    actorName: "Amina",
    ...overrides,
  } as Notification;
}

describe("describeNotification", () => {
  it("names an event's group by the event and its newest news, not by the server", () => {
    const grouped = notification({
      reason: "event_reminder",
      detail: "Intro to Go workshop",
      actor: "localhost",
      actorName: "",
      count: 2,
      actors: 2,
    });
    expect(describeNotification(grouped, say)).toBe("Starting soon: Intro to Go workshop — 2 new");
  });

  it("still names the people in a group of messages", () => {
    const grouped = notification({ reason: "mention", count: 3, actors: 2 });
    expect(describeNotification(grouped, say)).toBe("Amina and 1 other — 3 new");
  });

  it("says a single event notification as it is", () => {
    const single = notification({ reason: "event_promoted", detail: "Intro to Go workshop" });
    expect(describeNotification(single, say)).toBe(
      "A place freed up, so you are going to Intro to Go workshop",
    );
  });
});
