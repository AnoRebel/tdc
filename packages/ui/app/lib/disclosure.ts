import type { RoomMember } from "@tdc/types";

/**
 * Reading a member list without filling in what somebody withheld.
 *
 * The server omits a detail a person has chosen not to show, so the field is
 * absent rather than false or empty. The temptation on this side is to supply a
 * default — `member.online ?? false` reads naturally and is exactly wrong:
 * showing "offline" for somebody who withheld their presence discloses the
 * thing they withheld, and a viewer cannot tell it from somebody genuinely
 * offline.
 *
 * These return undefined for "not disclosed" and leave the interface to say
 * nothing at all in that case.
 */

/** What a member's presence is, or undefined when they do not show it. */
export function presenceOf(member: RoomMember): "online" | "offline" | undefined {
  if (member.online === undefined) return undefined;
  return member.online ? "online" : "offline";
}

/** A member's status, or undefined when they do not show it. */
export function statusOf(member: RoomMember): string | undefined {
  return member.status === undefined || member.status === "" ? undefined : member.status;
}

/** When a member was last here, or undefined when they do not show it. */
export function lastSeenOf(member: RoomMember): Date | undefined {
  if (!member.lastSeenAt) return undefined;
  const at = new Date(member.lastSeenAt);
  return Number.isNaN(at.getTime()) ? undefined : at;
}

/**
 * Whether this member list is a moderator's view of withheld details.
 *
 * The server sends `withheld` only to a moderator who asked to moderate and is
 * being shown what it names. Its presence is what lets the interface mark the
 * details as an exception rather than showing them as ordinary information.
 */
export function withheldFrom(member: RoomMember): string[] {
  return member.withheld ?? [];
}
