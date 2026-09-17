/**
 * Between what WebAuthn's browser API speaks and what the wire does.
 *
 * Every binary field — the challenge, the credential id, the user handle, the
 * signature — is an ArrayBuffer in `navigator.credentials` and base64url in
 * JSON. Converting wrongly does not fail loudly: the ceremony completes, the
 * signature does not verify, and nothing says which field was mangled. That is
 * why these are separated out and tested rather than left inline.
 */

/** base64url to bytes. */
export function decodeBase64Url(value: string): ArrayBuffer {
  // base64url is base64 with "+/" swapped for "-_" and the padding dropped, so
  // both are put back before the browser's own decoder sees it.
  const base64 = value.replace(/-/g, "+").replace(/_/g, "/");
  const padded = base64.padEnd(base64.length + ((4 - (base64.length % 4)) % 4), "=");

  const binary = atob(padded);
  const bytes = new Uint8Array(binary.length);
  for (let index = 0; index < binary.length; index += 1) {
    bytes[index] = binary.charCodeAt(index);
  }
  return bytes.buffer;
}

/** Bytes to base64url. */
export function encodeBase64Url(buffer: ArrayBuffer): string {
  const bytes = new Uint8Array(buffer);
  let binary = "";
  for (const byte of bytes) binary += String.fromCharCode(byte);

  return btoa(binary).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}
