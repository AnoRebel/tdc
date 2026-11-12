import { describe, expect, it } from "vitest";

import { lastSeenOf, presenceOf, statusOf, withheldFrom } from "./disclosure";

import type { RoomMember } from "@tdc/types";

function member(extra: Partial<RoomMember> = {}): RoomMember {
  return {
    id: "1",
    username: "juma",
    displayName: "Juma",
    address: "juma@tdc.tz",
    joinedAt: "2026-01-01T00:00:00Z",
    ...extra,
  };
}

describe("what a member list does not say", () => {
  /*
   * The failure this exists to catch is `member.online ?? false`, which reads
   * naturally and discloses the thing that was withheld: a viewer cannot tell
   * "chose not to say" from "offline".
   */
  it("does not turn a withheld presence into offline", () => {
    expect(presenceOf(member())).toBeUndefined();
  });

  it("still reports a presence that was disclosed", () => {
    expect(presenceOf(member({ online: true }))).toBe("online");
    expect(presenceOf(member({ online: false }))).toBe("offline");
  });

  it("does not turn a withheld status into an empty one", () => {
    expect(statusOf(member())).toBeUndefined();
    expect(statusOf(member({ status: "" }))).toBeUndefined();
    expect(statusOf(member({ status: "busy" }))).toBe("busy");
  });

  it("does not turn a withheld last-seen into a date", () => {
    expect(lastSeenOf(member())).toBeUndefined();
    expect(lastSeenOf(member({ lastSeenAt: "not a date" }))).toBeUndefined();
    expect(lastSeenOf(member({ lastSeenAt: "2026-01-02T03:04:05Z" }))).toBeInstanceOf(Date);
  });

  /*
   * Ordinary members are never told what somebody withholds — knowing that
   * a person hides their last-seen time is itself information about them — so
   * the absent field must read as "nothing withheld here to mark".
   */
  it("treats a missing withheld list as nothing to mark", () => {
    expect(withheldFrom(member())).toEqual([]);
    expect(withheldFrom(member({ withheld: ["lastSeen"] }))).toEqual(["lastSeen"]);
  });
});
