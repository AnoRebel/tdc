import { ApiError } from "@tdc/types";
import { describe, expect, it } from "vitest";

import {
  asksConfirmation,
  consequence,
  deciding,
  entryState,
  grouped,
  groupOf,
  isAdministrator,
  withEntry,
  type EffectivePermission,
  type Role,
} from "./roles";

function role(name: string, permissions: Role["permissions"], builtIn = false): Role {
  return { id: name, name, description: "", builtIn, memberCount: 0, permissions };
}

const member = role(
  "Member",
  [{ permission: "channel.create", granted: true, scope: "server" }],
  true,
);

describe("grouping the catalogue", () => {
  it("puts each permission where somebody would look for it", () => {
    expect(groupOf("channel.post")).toBe("messaging");
    expect(groupOf("message.delete.any")).toBe("messaging");
    expect(groupOf("channel.archive")).toBe("channels");
    expect(groupOf("channel.webhooks.manage")).toBe("integrations");
    expect(groupOf("server.applications.manage")).toBe("integrations");
    expect(groupOf("issue.assign")).toBe("projects");
    expect(groupOf("role.assign")).toBe("administration");
    expect(groupOf("user.suspend")).toBe("people");
    expect(groupOf("something.new")).toBe("other");
  });

  it("orders groups and leaves out the empty ones", () => {
    const sections = grouped([
      { name: "server.configure" },
      { name: "channel.post" },
      { name: "attachment.upload" },
    ]);
    expect(sections.map((section) => section.group)).toEqual(["messaging", "administration"]);
    expect(sections[0]!.permissions.map((p) => p.name)).toEqual([
      "attachment.upload",
      "channel.post",
    ]);
  });
});

describe("a role's entries", () => {
  const scoped = role("Keepers", [
    { permission: "channel.post", granted: false, scope: "server" },
    { permission: "channel.post", granted: true, scope: "channel", scopeId: "c1" },
  ]);

  it("are read per scope", () => {
    expect(entryState(scoped, "channel.post")).toBe("deny");
    expect(entryState(scoped, "channel.post", "c1")).toBe("grant");
    expect(entryState(scoped, "channel.post", "c2")).toBe("inherit");
    expect(entryState(undefined, "channel.post")).toBe("inherit");
  });

  it("change one scope without touching the other", () => {
    const cleared = withEntry(scoped, "channel.post", "inherit");
    expect(entryState(cleared, "channel.post")).toBe("inherit");
    expect(entryState(cleared, "channel.post", "c1")).toBe("grant");

    const denied = withEntry(scoped, "channel.post", "deny", { id: "c1" });
    expect(entryState(denied, "channel.post", "c1")).toBe("deny");
    expect(denied.permissions).toHaveLength(2);
  });
});

describe("what an entry will mean", () => {
  it("speaks of Member as the baseline", () => {
    expect(consequence(member, "channel.create", member)).toBe("baselineGrants");
    expect(consequence(member, "channel.archive", member)).toBe("baselineSilent");
  });

  it("says when Member already grants it", () => {
    const helpers = role("Helpers", [
      { permission: "channel.create", granted: true, scope: "server" },
    ]);
    expect(consequence(helpers, "channel.create", member)).toBe("alsoBaseline");
    expect(consequence(role("Empty", []), "channel.create", member)).toBe("fromBaseline");
    expect(consequence(role("Empty", []), "channel.archive", member)).toBe("notFromThisRole");
  });

  it("puts a denial above everything, administration included", () => {
    const admin = role("Admins", [
      { permission: "server.administer", granted: true, scope: "server" },
      { permission: "server.audit.read", granted: false, scope: "server" },
    ]);
    expect(consequence(admin, "server.audit.read", member)).toBe("denies");
    expect(consequence(admin, "user.suspend", member)).toBe("impliedByAdministration");
    expect(consequence(admin, "server.administer", member)).toBe("grants");
  });
});

describe("why somebody may", () => {
  const source = (
    roleName: string,
    granted: boolean,
    scope: "server" | "channel",
    baseline = false,
  ) => ({
    roleId: roleName,
    roleName,
    granted,
    scope,
    baseline,
  });

  it("sets Member aside where another role speaks at the same scope", () => {
    const permission: EffectivePermission = {
      name: "channel.post",
      allowed: true,
      basis: "scope",
      sources: [
        source("Member", false, "channel", true),
        source("Announcer", true, "channel"),
        source("Member", true, "server", true),
      ],
    };
    const { decided, overridden } = deciding(permission, false);
    expect(decided.map((s) => s.roleName)).toEqual(["Announcer"]);
    expect(overridden.map((s) => s.roleName)).toEqual(["Member"]);
  });

  it("names Member when nothing else speaks", () => {
    const permission: EffectivePermission = {
      name: "channel.post",
      allowed: false,
      basis: "scope",
      sources: [source("Member", false, "channel", true)],
    };
    expect(deciding(permission, false).decided.map((s) => s.roleName)).toEqual(["Member"]);
  });

  it("does not hold an administrator to the baseline", () => {
    const permission: EffectivePermission = {
      name: "channel.create",
      allowed: true,
      basis: "administrator",
      sources: [source("Member", false, "server", true)],
    };
    expect(deciding(permission, true).overridden).toHaveLength(1);
    expect(
      isAdministrator([
        {
          name: "server.administer",
          allowed: true,
          basis: "server",
          sources: [source("Owners", true, "server")],
        },
      ]),
    ).toBe(true);
    expect(
      isAdministrator([
        {
          name: "server.administer",
          allowed: true,
          basis: "server",
          sources: [source("Member", true, "server", true)],
        },
      ]),
    ).toBe(false);
  });
});

describe("a refusal that asks", () => {
  it("is told apart from one that does not", () => {
    const asking = new ApiError(
      {
        code: "conflict",
        message: "this would remove your own authority; send it again with confirmed to go ahead",
      },
      409,
    );
    const final = new ApiError(
      { code: "conflict", message: "this would leave the server with no administrator" },
      409,
    );
    expect(asksConfirmation(asking)).toBe(true);
    expect(asksConfirmation(final)).toBe(false);
  });
});
