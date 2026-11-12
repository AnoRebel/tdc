/**
 * What an API token may do, worked out the way the server works it out.
 *
 * A token is its account with everything outside its scope denied. So the
 * question the screen asks is not "what to grant" but "what to leave this
 * program", and the answer is always a subset of what the account holds —
 * the server refuses anything more.
 */

/** The areas permissions are grouped under, by the first part of their name. */
export const areas = [
  "channel",
  "message",
  "attachment",
  "call",
  "project",
  "issue",
  "directory",
  "moderation",
  "user",
  "role",
  "server",
] as const;

export type Area = (typeof areas)[number] | "other";

/** The area a permission belongs to: "channel.post" is a channel's. */
export function areaOf(permission: string): Area {
  const head = permission.split(".")[0] ?? "";
  return (areas as readonly string[]).includes(head) ? (head as Area) : "other";
}

/**
 * The permissions an account holds, grouped by area in a fixed order, so the
 * list reads the same on every visit and a long one can be scanned.
 */
export function groupPermissions(held: string[]): { area: Area; permissions: string[] }[] {
  const order: Area[] = [...areas, "other"];
  return order
    .map((area) => ({
      area,
      permissions: held.filter((permission) => areaOf(permission) === area).sort(),
    }))
    .filter((group) => group.permissions.length > 0);
}

/**
 * A starting point for the commonest program: one that reads and posts.
 * Only what the account actually holds, since anything else would be refused.
 */
export function readAndPost(held: string[]): string[] {
  return ["channel.read", "channel.post", "attachment.upload"].filter((p) => held.includes(p));
}

/** What a token will be denied: everything the account holds and it was not left. */
export function deniedBy(held: string[], scope: string[]): string[] {
  const kept = new Set(scope);
  return held.filter((permission) => !kept.has(permission));
}

/** Days until a chosen lifetime ends, or undefined for a token that does not expire. */
export const lifetimes = [undefined, 1, 7, 30, 90, 365] as const;

export function expiryFrom(days: number | undefined, now: Date = new Date()): Date | undefined {
  return days === undefined ? undefined : new Date(now.getTime() + days * 86_400_000);
}
