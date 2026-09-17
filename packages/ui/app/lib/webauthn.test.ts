import { describe, expect, it } from "vitest";

import { decodeBase64Url, encodeBase64Url } from "./webauthn";

/*
 * The conversion nothing else catches.
 *
 * A mangled binary field does not fail loudly: the ceremony completes, the
 * signature does not verify, and no message says which of the challenge, the
 * credential id, the user handle or the signature was wrong. A round trip here
 * is cheap; finding it from a failed sign-in is not.
 */

/** Bytes, as an ArrayBuffer, without depending on either function above. */
function bytes(...values: number[]): ArrayBuffer {
  return new Uint8Array(values).buffer;
}

function readBack(buffer: ArrayBuffer): number[] {
  return Array.from(new Uint8Array(buffer));
}

describe("base64url, as WebAuthn uses it", () => {
  /*
   * Every byte value, which is the case that matters: a challenge is random,
   * so any of the 256 can appear, and an encoder that mishandles one produces
   * a signature failure on roughly one sign-in in every few.
   */
  it("round-trips every byte value", () => {
    const all = new Uint8Array(256);
    for (let value = 0; value < 256; value += 1) all[value] = value;

    expect(readBack(decodeBase64Url(encodeBase64Url(all.buffer)))).toEqual(Array.from(all));
  });

  /*
   * The two characters base64url substitutes.
   *
   * 0xFB 0xEF encodes to "++8" in base64 and "--8" in base64url; 0xFF 0xFF to
   * "//8" and "__8". Getting the substitution backwards, or only doing it in
   * one direction, survives most test data and fails on these.
   */
  it("uses - and _ rather than + and /", () => {
    const encoded = encodeBase64Url(bytes(0xfb, 0xef, 0xff, 0xff));

    expect(encoded).not.toContain("+");
    expect(encoded).not.toContain("/");
    expect(readBack(decodeBase64Url(encoded))).toEqual([0xfb, 0xef, 0xff, 0xff]);
  });

  /*
   * Padding is dropped on the way out and restored on the way in.
   *
   * WebAuthn's JSON form is unpadded. An encoder that leaves "=" on produces a
   * value the server decodes differently or refuses; a decoder that cannot
   * restore it fails on every length that is not a multiple of three.
   */
  it("drops padding when encoding and restores it when decoding", () => {
    for (const length of [1, 2, 3, 4, 5, 32, 33, 64]) {
      const value = new Uint8Array(length).fill(0xab).buffer;
      const encoded = encodeBase64Url(value);

      expect(encoded, `length ${length}`).not.toContain("=");
      expect(readBack(decodeBase64Url(encoded)), `length ${length}`).toHaveLength(length);
    }
  });

  // A 32-byte challenge is the ordinary case, and the one every ceremony
  // begins with.
  it("round-trips a challenge-sized value", () => {
    const challenge = new Uint8Array(32);
    for (let index = 0; index < 32; index += 1) challenge[index] = (index * 7 + 13) % 256;

    expect(readBack(decodeBase64Url(encodeBase64Url(challenge.buffer)))).toEqual(
      Array.from(challenge),
    );
  });

  // An empty value, which is what an absent user handle looks like before it is
  // omitted altogether.
  it("handles nothing at all", () => {
    expect(encodeBase64Url(new ArrayBuffer(0))).toBe("");
    expect(readBack(decodeBase64Url(""))).toEqual([]);
  });
});
