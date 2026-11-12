/*
 * What somebody is called, checked before the server is asked.
 *
 * These mirror the server's rules so a person is told at once what is wrong
 * with what they typed, instead of after a round trip. The server checks again
 * and is the authority — the reserved names, and whether somebody else holds
 * a name, are only known there.
 */

import { shallowReactive, toRaw } from "vue";

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

/**
 * The name to call somebody by wherever one person is named to another: their
 * display name, or their username when they have not set one. A guest's
 * username is generated, so the name they gave is always their display name.
 */
export function nameOf(person: { username: string; displayName?: string | null }): string {
  return person.displayName?.trim() || person.username;
}

/*
 * What people are called, by address.
 *
 * Messages, reactions and receipts name people by address, which is what is
 * signed and what stays put; what to call them comes separately — with each
 * message, and with every list of people this client is shown. Remembered here
 * so a name learned from one is used by all the others, and reactive so a
 * reaction drawn before the member list arrived is renamed when it does.
 */
const known = shallowReactive(new Map<string, string>());

/** Remembers what the person at an address is called. */
export function learnName(address: string | undefined, name: string | undefined) {
  const trimmed = name?.trim();
  // Compared without tracking: this runs while a timeline is being folded, and
  // reading the book there would make every fold depend on every name.
  if (address && trimmed && toRaw(known).get(address) !== trimmed) known.set(address, trimmed);
}

/** Remembers what each of these people is called. */
export function learnNames(
  people: readonly { address?: string; username: string; displayName?: string | null }[],
) {
  for (const person of people) learnName(person.address, nameOf(person));
}

/**
 * What to call the person at an address: the name this client has been told,
 * or the address's first half — their username here, or the only name a person
 * on another server is known by.
 */
export function nameAt(address: string): string {
  return known.get(address) ?? address.split("@")[0] ?? address;
}

/**
 * What to call somebody mentioned as "@username" on this server, when this
 * client has been told. Usernames are matched without regard to case, as the
 * server matches a mention.
 */
export function mentionedName(username: string, server: string): string | undefined {
  const exact = known.get(`${username}@${server}`);
  if (exact) return exact;
  const wanted = `${username}@${server}`.toLowerCase();
  for (const [address, name] of known) if (address.toLowerCase() === wanted) return name;
  return undefined;
}
