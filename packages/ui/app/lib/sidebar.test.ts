import type { NotificationPreferences, Room } from "@tdc/types";
import { describe, expect, it } from "vitest";

import {
  arrangeSidebar,
  movePinned,
  otherParticipants,
  settingFor,
  showsUnreadBadge,
} from "./sidebar";

function room(id: string, kind: Room["kind"] = "channel", name = id): Room {
  return {
    id,
    slug: id,
    name,
    topic: "",
    kind,
    visibility: "public",
    address: `${id}@example`,
    archived: false,
    member: true,
  } as Room;
}

describe("arranging the sidebar", () => {
  const general = room("general");
  const random = room("random");
  const amina = room("dm-amina", "direct", "Amina, Ano");
  const later = room("later");

  it("moves pinned conversations to the top, in the chosen order", () => {
    const groups = arrangeSidebar([general, random], [amina], [later], ["dm-amina", "random"]);

    expect(groups.pinned.map((r) => r.id)).toEqual(["dm-amina", "random"]);
    expect(groups.channels.map((r) => r.id)).toEqual(["general"]);
    expect(groups.direct).toEqual([]);
    expect(groups.browse.map((r) => r.id)).toEqual(["later"]);
  });

  it("shows every conversation exactly once", () => {
    const groups = arrangeSidebar([general, random], [amina], [later], ["general", "later"]);
    const all = [...groups.pinned, ...groups.channels, ...groups.direct, ...groups.browse];

    expect(all.map((r) => r.id).sort()).toEqual(["dm-amina", "general", "later", "random"]);
  });

  it("does not draw a pin on a conversation that is no longer there", () => {
    const groups = arrangeSidebar([general], [], [], ["gone", "general"]);

    expect(groups.pinned.map((r) => r.id)).toEqual(["general"]);
  });
});

describe("reordering what is pinned", () => {
  it("swaps a conversation with its neighbour", () => {
    expect(movePinned(["a", "b", "c"], "b", -1)).toEqual(["b", "a", "c"]);
    expect(movePinned(["a", "b", "c"], "b", 1)).toEqual(["a", "c", "b"]);
  });

  it("stops at either end rather than wrapping", () => {
    const ids = ["a", "b"];
    expect(movePinned(ids, "a", -1)).toBe(ids);
    expect(movePinned(ids, "b", 1)).toBe(ids);
    expect(movePinned(ids, "missing", 1)).toBe(ids);
  });
});

describe("naming a direct conversation", () => {
  it("leaves out the reader", () => {
    expect(otherParticipants({ name: "Amina, Ano, Baraka" }, "Ano")).toEqual(["Amina", "Baraka"]);
  });

  it("keeps the name of a conversation with only oneself", () => {
    expect(otherParticipants({ name: "Ano" }, "Ano")).toEqual(["Ano"]);
  });
});

describe("muted conversations", () => {
  const preferences: NotificationPreferences = {
    default: { level: "mentions", muted: false },
    rooms: { quiet: { level: "all", muted: true } },
  };

  it("use their own setting, and the default otherwise", () => {
    expect(settingFor(preferences, "quiet")).toEqual({ level: "all", muted: true });
    expect(settingFor(preferences, "loud")).toEqual({ level: "mentions", muted: false });
  });

  it("show no unread badge", () => {
    expect(showsUnreadBadge(preferences, "quiet", 4)).toBe(false);
    expect(showsUnreadBadge(preferences, "loud", 4)).toBe(true);
    expect(showsUnreadBadge(preferences, "loud", 0)).toBe(false);
  });
});
