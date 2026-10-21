import { describe, expect, it } from "vitest";

import { areaOf, deniedBy, expiryFrom, groupPermissions, readAndPost } from "./tokens";

describe("what a token may do", () => {
  const held = ["issue.create", "channel.post", "channel.read", "server.configure", "odd"];

  it("groups the account's permissions by area, in a fixed order", () => {
    expect(groupPermissions(held)).toEqual([
      { area: "channel", permissions: ["channel.post", "channel.read"] },
      { area: "issue", permissions: ["issue.create"] },
      { area: "server", permissions: ["server.configure"] },
      { area: "other", permissions: ["odd"] },
    ]);
    expect(areaOf("message.edit.any")).toBe("message");
  });

  it("denies everything the account holds and the token was not left", () => {
    expect(deniedBy(held, ["channel.read"])).toEqual([
      "issue.create",
      "channel.post",
      "server.configure",
      "odd",
    ]);
    expect(deniedBy(held, held)).toEqual([]);
  });

  it("offers reading and posting only as far as the account can", () => {
    expect(readAndPost(held)).toEqual(["channel.read", "channel.post"]);
  });

  it("ends a chosen number of days from now, or never", () => {
    const now = new Date("2026-10-07T12:00:00Z");
    expect(expiryFrom(undefined, now)).toBeUndefined();
    expect(expiryFrom(7, now)?.toISOString()).toBe("2026-10-14T12:00:00.000Z");
  });
});
