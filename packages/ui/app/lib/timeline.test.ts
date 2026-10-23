import type { Message } from "@tdc/types";
import { describe, expect, it } from "vitest";

import { completeMention, foldMessages, groupMessages, mentionAt } from "./timeline";

let sequence = 0;
function event(type: string, content: Message["content"], extra: Partial<Message> = {}): Message {
  sequence += 1;
  return {
    id: extra.id ?? `e${sequence}`,
    type,
    sender: "amina@tdc.test",
    sequence,
    content,
    sentAt: new Date(Date.UTC(2026, 9, 1, 9, 0, sequence)).toISOString(),
    redacted: false,
    ...extra,
  };
}

describe("folding a conversation", () => {
  it("applies an edit, a deletion and a program's update to what they name", () => {
    const shown = foldMessages([
      event("message.sent", { body: "first" }, { id: "a" }),
      event("message.sent", { body: "second" }, { id: "b" }),
      event("message.sent", { body: "ask", actions: [{ name: "yes", label: "Yes" }] }, { id: "c" }),
      event("message.edited", { body: "first, corrected", replaces: "a" }),
      event("message.deleted", { redacts: "b" }),
      event("message.updated", {
        updates: "c",
        version: 2,
        body: "asked",
        actions: [],
      } as unknown as Message["content"]),
    ]);

    expect(shown.map((message) => message.id)).toEqual(["a", "b", "c"]);
    expect(shown[0]).toMatchObject({ body: "first, corrected", edited: true, removed: false });
    expect(shown[1]).toMatchObject({ removed: true, actions: [] });
    expect(shown[2]).toMatchObject({ body: "asked", actions: [], controlsVersion: 2 });
  });

  it("keeps the highest version of a program's update, whatever order they arrive in", () => {
    const shown = foldMessages([
      event("message.sent", { body: "q" }, { id: "q" }),
      event("message.updated", {
        updates: "q",
        version: 3,
        body: "third",
      } as unknown as Message["content"]),
      event("message.updated", {
        updates: "q",
        version: 2,
        body: "second",
      } as unknown as Message["content"]),
    ]);
    expect(shown[0]!.body).toBe("third");
  });
});

describe("grouping messages into turns", () => {
  it("joins one person's messages close together, and splits after a pause", () => {
    const at = (minutes: number) => new Date(Date.UTC(2026, 9, 1, 9, minutes)).toISOString();
    const shown = foldMessages([
      event("message.sent", { body: "1" }, { sentAt: at(0) }),
      event("message.sent", { body: "2" }, { sentAt: at(2) }),
      event("message.sent", { body: "3" }, { sentAt: at(20) }),
      event("message.sent", { body: "4" }, { sentAt: at(21), sender: "juma@tdc.test" }),
    ]);

    const runs = groupMessages(shown, (sender) => sender === "juma@tdc.test");
    expect(runs.map((run) => run.items.length)).toEqual([2, 1, 1]);
    expect(runs.map((run) => run.mine)).toEqual([false, false, true]);
  });
});

describe("noticing a mention being typed", () => {
  it("finds the name after an @ at the start of a word", () => {
    expect(mentionAt("hi @am", 6)).toEqual({ query: "am", start: 3 });
    expect(mentionAt("@", 1)).toEqual({ query: "", start: 0 });
  });

  it("ignores an email address and anything inside code", () => {
    expect(mentionAt("mail amina@tdc", 14)).toBeNull();
    expect(mentionAt("`@Comp", 6)).toBeNull();
    expect(mentionAt("`x` @ju", 7)).toEqual({ query: "ju", start: 4 });
  });

  it("reads only what is before the caret", () => {
    expect(mentionAt("@ju and more", 3)).toEqual({ query: "ju", start: 0 });
    expect(mentionAt("@ju and more", 8)).toBeNull();
  });

  it("puts the chosen name in place, with one space after it", () => {
    const text = "hi @am there";
    const mention = mentionAt(text, 6)!;
    expect(completeMention(text, mention, 6, "amina")).toEqual({
      text: "hi @amina there",
      caret: 10,
    });
    expect(completeMention("@j", mentionAt("@j", 2)!, 2, "juma")).toEqual({
      text: "@juma ",
      caret: 6,
    });
  });
});
