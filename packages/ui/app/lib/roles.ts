/**
 * The rules behind the roles screen, kept apart from it so they can be tested.
 *
 * Nothing here decides anything the server does not decide again. What it does
 * is say, before somebody clicks, what a click will mean — which, under a model
 * with a baseline role, denials that beat grants and an administrator who is
 * exempt from the baseline, is not something anybody should have to work out.
 */

/** A permission, as the server's catalogue describes it. */
export interface PermissionDefinition {
  name: string;
  description: string;
  /** Where it may be granted: "server", "channel", "project". */
  scopes: string[];
  administrative: boolean;
}

/** One thing a role says about one permission. */
export interface RoleEntry {
  permission: string;
  /** False is an explicit denial. */
  granted: boolean;
  scope: "server" | "channel" | "project";
  scopeId?: string;
  scopeName?: string;
}

export interface Role {
  id: string;
  name: string;
  description: string;
  builtIn: boolean;
  memberCount: number;
  permissions: RoleEntry[];
}

/** What a role says about a permission: grants it, denies it, or nothing. */
export type EntryState = "grant" | "deny" | "inherit";

/** The role every account holds, whose entries are a baseline beneath the rest. */
export const BASELINE_ROLE = "Member";
export const ADMINISTER = "server.administer";

export function isBaseline(role: Pick<Role, "name" | "builtIn">): boolean {
  return role.builtIn && role.name === BASELINE_ROLE;
}

/** What a role says about a permission, server-wide or in one channel. */
export function entryState(
  role: Role | undefined,
  permission: string,
  scopeId?: string,
): EntryState {
  const entry = role?.permissions.find(
    (candidate) =>
      candidate.permission === permission &&
      (scopeId ? candidate.scopeId === scopeId : candidate.scope === "server"),
  );
  if (!entry) return "inherit";
  return entry.granted ? "grant" : "deny";
}

/** The role with one entry changed, for an optimistic update. */
export function withEntry(
  role: Role,
  permission: string,
  state: EntryState,
  scope?: { id: string; name?: string },
): Role {
  const others = role.permissions.filter(
    (entry) =>
      !(
        entry.permission === permission &&
        (scope ? entry.scopeId === scope.id : entry.scope === "server")
      ),
  );
  if (state === "inherit") return { ...role, permissions: others };
  return {
    ...role,
    permissions: [
      ...others,
      scope
        ? {
            permission,
            granted: state === "grant",
            scope: "channel",
            scopeId: scope.id,
            scopeName: scope.name,
          }
        : { permission, granted: state === "grant", scope: "server" },
    ],
  };
}

/*
 * The areas permissions are shown in.
 *
 * Derived from the name rather than listed, so a permission the server adds
 * tomorrow lands somewhere sensible without this file changing — and anything
 * that matches nothing is shown under "other" rather than not at all.
 */
export const GROUPS = [
  "messaging",
  "channels",
  "moderation",
  "people",
  "projects",
  "directory",
  "calls",
  "integrations",
  "administration",
  "other",
] as const;
export type PermissionGroup = (typeof GROUPS)[number];

export function groupOf(name: string): PermissionGroup {
  if (name === "channel.webhooks.manage" || name === "server.applications.manage")
    return "integrations";
  if (name === "channel.read" || name === "channel.post") return "messaging";
  if (name.startsWith("message.") || name.startsWith("attachment.")) return "messaging";
  if (name.startsWith("channel.")) return "channels";
  if (name.startsWith("moderation.")) return "moderation";
  if (name.startsWith("user.")) return "people";
  if (name.startsWith("project.") || name.startsWith("issue.")) return "projects";
  if (name.startsWith("directory.")) return "directory";
  if (name.startsWith("call.")) return "calls";
  if (name.startsWith("server.") || name.startsWith("role.")) return "administration";
  return "other";
}

/** The catalogue in its groups, in a fixed order, empty groups left out. */
export function grouped<T extends { name: string }>(
  definitions: T[],
): { group: PermissionGroup; permissions: T[] }[] {
  return GROUPS.map((group) => ({
    group,
    permissions: definitions
      .filter((definition) => groupOf(definition.name) === group)
      .sort((a, b) => a.name.localeCompare(b.name)),
  })).filter((section) => section.permissions.length > 0);
}

/** Whether a role grants server administration, which implies everything else. */
export function administers(role: Role | undefined): boolean {
  return entryState(role, ADMINISTER) === "grant";
}

/**
 * What a role's entry for a permission will mean, said before it is changed.
 *
 * A key under `roles.consequence` in the locale files. Server-wide only: a
 * channel's entries are shown in the channel, where the question is narrower.
 */
export function consequence(role: Role, permission: string, member: Role | undefined): string {
  const state = entryState(role, permission);

  if (isBaseline(role)) {
    return { grant: "baselineGrants", deny: "baselineDenies", inherit: "baselineSilent" }[state];
  }

  // A denial beats every grant, administration's included.
  if (state === "deny") return "denies";

  if (permission !== ADMINISTER && administers(role)) return "impliedByAdministration";

  const baseline = entryState(member, permission);
  if (state === "grant") return baseline === "grant" ? "alsoBaseline" : "grants";
  return baseline === "grant" ? "fromBaseline" : "notFromThisRole";
}

/** One role's entry behind somebody's answer, as the server reports it. */
export interface PermissionSource {
  roleId: string;
  roleName: string;
  granted: boolean;
  scope: "server" | "channel";
  baseline: boolean;
}

/** Somebody's answer for one permission. */
export interface EffectivePermission {
  name: string;
  allowed: boolean;
  basis: "administrator" | "server" | "scope" | "none";
  sources: PermissionSource[];
}

/**
 * Which entries decided somebody's answer, and which were set aside.
 *
 * The same rules the server applies: at the scope that decided, Member's entry
 * counts only where no other role says anything there, and not at all for
 * somebody another role makes an administrator. Of what counts, the entries
 * pointing the way the answer went are the ones that decided it.
 */
export function deciding(
  permission: EffectivePermission,
  administrator: boolean,
): { decided: PermissionSource[]; overridden: PermissionSource[] } {
  if (permission.basis === "administrator" || permission.basis === "none") {
    return { decided: [], overridden: permission.sources.filter((source) => source.baseline) };
  }

  const scope = permission.basis === "scope" ? "channel" : "server";
  const here = permission.sources.filter((source) => source.scope === scope);
  const others = here.filter((source) => !source.baseline);
  const counted = others.length > 0 || administrator ? others : here;

  return {
    decided: counted.filter((source) => source.granted === permission.allowed),
    overridden: here.filter((source) => source.baseline && !counted.includes(source)),
  };
}

/** Whether somebody holds administration through a role other than the baseline. */
export function isAdministrator(permissions: EffectivePermission[]): boolean {
  const administer = permissions.find((permission) => permission.name === ADMINISTER);
  return Boolean(
    administer?.sources.some(
      (source) => !source.baseline && source.scope === "server" && source.granted,
    ),
  );
}

/**
 * Whether a refusal is the server asking "are you sure" rather than saying no.
 *
 * The server refuses two administrative changes the same way, with a conflict:
 * one that would leave nobody administering it, which is final, and one that
 * would take the asker's own authority, which they may confirm. Its message is
 * what tells them apart.
 */
export function asksConfirmation(error: unknown): boolean {
  if (!(error instanceof Error)) return false;
  return (error as { status?: number }).status === 409 && /your own authority/.test(error.message);
}
