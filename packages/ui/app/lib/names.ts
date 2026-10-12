/*
 * What somebody is called, checked before the server is asked.
 *
 * These mirror the server's rules so a person is told at once what is wrong
 * with what they typed, instead of after a round trip. The server checks again
 * and is the authority — the reserved names, and whether somebody else holds
 * a name, are only known there.
 */

export const usernameLength = { min: 2, max: 32 } as const;
export const displayNameMax = 64;

export type UsernameProblem = "length" | "characters" | "edges";

/** What is wrong with a username's shape, if anything. */
export function usernameProblem(value: string): UsernameProblem | null {
  const name = value.trim();
  const length = [...name].length;
  if (length < usernameLength.min || length > usernameLength.max) return "length";
  if (!/^[A-Za-z0-9._-]+$/.test(name)) return "characters";
  if (/^[._-]|[._-]$/.test(name)) return "edges";
  return null;
}

/*
 * Characters a display name may not carry: controls, and the invisible
 * formatting ones (Unicode Cf) — right-to-left overrides that make a name read
 * backwards, zero-width joiners that make two names look the same.
 */
const unshowable = /[\p{Cc}\p{Cf}]/u;

/*
 * The joiner inside an emoji sequence (woman, joiner, laptop) is a format character too, and the
 * harmless one: allowed after a symbol, never between letters.
 */
const emojiJoiner = /(?<=[\p{S}\uFE0F])\u200D/gu;

export type DisplayNameProblem = "length" | "characters";

/** What is wrong with a display name, if anything. Empty is allowed. */
export function displayNameProblem(value: string): DisplayNameProblem | null {
  const name = value.trim();
  if ([...name].length > displayNameMax) return "length";
  if (unshowable.test(name.replace(emojiJoiner, ""))) return "characters";
  return null;
}

/**
 * Whether an address is this person's, now or under a name they had.
 *
 * Messages are signed with the address they were sent under, so somebody who
 * changed their username still wrote everything they wrote before; comparing
 * only the current address would hand their own history to a stranger.
 */
export function isOwnAddress(
  user: { address: string; formerAddresses?: readonly string[] } | null | undefined,
  address: string,
): boolean {
  if (!user || !address) return false;
  return address === user.address || (user.formerAddresses ?? []).includes(address);
}
