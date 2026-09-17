import type { Passkey, SignInResult } from "@tdc/types";
import { decodeBase64Url as decode, encodeBase64Url as encode } from "#layers/ui/app/lib/webauthn";

/**
 * Signing in with something held rather than something known.
 *
 * The browser's WebAuthn API speaks ArrayBuffers and the wire speaks base64url,
 * so most of this is that conversion. It is done here rather than in each
 * client because getting it wrong produces a ceremony that fails verification
 * with no useful message — the signature simply does not match, and nothing
 * says which field was mangled.
 */
export function usePasskeys() {
  const api = useApi();
  const { info } = useServerInfo();

  /**
   * Whether this server offers passkeys at all.
   *
   * A server with no canonical address cannot: a passkey is bound to a
   * hostname. Drawing the button anyway would offer something that answers 404.
   */
  const offered = computed(() => info.value?.passkeys === true);

  /**
   * Whether this browser can do them.
   *
   * Separate from whether the server offers them, and both must hold. An old
   * browser, or any page not served over TLS, has no credentials API at all.
   */
  const supported = computed(() => {
    if (import.meta.server) return false;
    return typeof window.PublicKeyCredential === "function";
  });

  /** Both sides agreeing, which is when a button is worth drawing. */
  const available = computed(() => offered.value && supported.value);

  /**
   * Enrol a passkey on the account that is already signed in.
   *
   * label is what it will be called in the list. The authenticator may refuse,
   * or the person may dismiss its prompt; both arrive as a thrown error and
   * mean the same thing — no passkey was made.
   */
  async function enrol(label: string): Promise<Passkey> {
    const options =
      await api.post<PublicKeyCredentialCreationOptionsJSON>("/api/me/passkeys/begin");

    const created = (await navigator.credentials.create({
      publicKey: fromCreationJSON(options),
    })) as PublicKeyCredential | null;

    if (!created) throw new Error("No passkey was created.");

    /*
     * The name goes in the query, not the body.
     *
     * The body is the authenticator's own response, which the server parses as
     * the specification defines it; adding a field would mean reshaping what
     * the browser just made.
     */
    return api.post<Passkey>(
      `/api/me/passkeys/finish?label=${encodeURIComponent(label)}`,
      toAttestationJSON(created),
    );
  }

  /**
   * Sign in with whichever authenticator is present.
   *
   * Nothing is named: the authenticator offers what it holds for this server,
   * which is what makes this one gesture rather than a username and a gesture.
   */
  async function signIn(): Promise<SignInResult> {
    const options = await api.post<PublicKeyCredentialRequestOptionsJSON>(
      "/api/auth/passkey/signin/begin",
    );

    const asserted = (await navigator.credentials.get({
      publicKey: fromRequestJSON(options),
    })) as PublicKeyCredential | null;

    if (!asserted) throw new Error("No passkey was offered.");

    return api.post<SignInResult>("/api/auth/passkey/signin/finish", toAssertionJSON(asserted));
  }

  /** Somebody's own passkeys. */
  async function list(): Promise<Passkey[]> {
    return api.get<Passkey[]>("/api/me/passkeys");
  }

  /** Change what one is called. */
  async function rename(id: string, label: string): Promise<Passkey> {
    return api.patch<Passkey>(`/api/me/passkeys/${encodeURIComponent(id)}`, { label });
  }

  /**
   * Remove one.
   *
   * Refused where it is the only way left into the account, which the server
   * says with an explanation rather than a bare refusal.
   */
  async function revoke(id: string): Promise<void> {
    await api.delete(`/api/me/passkeys/${encodeURIComponent(id)}`);
  }

  return { offered, supported, available, enrol, signIn, list, rename, revoke };
}

/*
 * What the server sends, in the shape the specification defines for JSON.
 *
 * Every binary field is base64url on the wire and an ArrayBuffer in the API, so
 * these types name the former and the converters below produce the latter.
 */
interface PublicKeyCredentialCreationOptionsJSON {
  publicKey?: PublicKeyCredentialCreationOptionsJSON;
  challenge: string;
  rp: { id?: string; name: string };
  user: { id: string; name: string; displayName: string };
  pubKeyCredParams: PublicKeyCredentialParameters[];
  timeout?: number;
  excludeCredentials?: { id: string; type: string; transports?: string[] }[];
  authenticatorSelection?: AuthenticatorSelectionCriteria;
  attestation?: string;
}

interface PublicKeyCredentialRequestOptionsJSON {
  publicKey?: PublicKeyCredentialRequestOptionsJSON;
  challenge: string;
  timeout?: number;
  rpId?: string;
  allowCredentials?: { id: string; type: string; transports?: string[] }[];
  userVerification?: string;
}

/*
 * The server may wrap its options in a `publicKey` key or send them bare,
 * depending on the library version. Unwrapping here means neither client cares.
 */
function unwrap<T extends { publicKey?: T }>(options: T): T {
  return options.publicKey ?? options;
}

function fromCreationJSON(
  options: PublicKeyCredentialCreationOptionsJSON,
): PublicKeyCredentialCreationOptions {
  const source = unwrap(options);

  return {
    ...source,
    challenge: decode(source.challenge),
    user: { ...source.user, id: decode(source.user.id) },
    excludeCredentials: source.excludeCredentials?.map((credential) => ({
      ...credential,
      id: decode(credential.id),
      type: "public-key",
      transports: credential.transports as AuthenticatorTransport[] | undefined,
    })),
  } as PublicKeyCredentialCreationOptions;
}

function fromRequestJSON(
  options: PublicKeyCredentialRequestOptionsJSON,
): PublicKeyCredentialRequestOptions {
  const source = unwrap(options);

  return {
    ...source,
    challenge: decode(source.challenge),
    allowCredentials: source.allowCredentials?.map((credential) => ({
      ...credential,
      id: decode(credential.id),
      type: "public-key",
      transports: credential.transports as AuthenticatorTransport[] | undefined,
    })),
  } as PublicKeyCredentialRequestOptions;
}

/** What the server expects back from an enrolment. */
function toAttestationJSON(credential: PublicKeyCredential) {
  const response = credential.response as AuthenticatorAttestationResponse;

  return {
    id: credential.id,
    rawId: encode(credential.rawId),
    type: credential.type,
    response: {
      clientDataJSON: encode(response.clientDataJSON),
      attestationObject: encode(response.attestationObject),
      // What the authenticator can be reached by — USB, NFC, internal. The
      // server records it so a later sign-in can hint at where to look.
      transports: response.getTransports?.() ?? [],
    },
    clientExtensionResults: credential.getClientExtensionResults(),
  };
}

/** What the server expects back from a sign-in. */
function toAssertionJSON(credential: PublicKeyCredential) {
  const response = credential.response as AuthenticatorAssertionResponse;

  return {
    id: credential.id,
    rawId: encode(credential.rawId),
    type: credential.type,
    response: {
      clientDataJSON: encode(response.clientDataJSON),
      authenticatorData: encode(response.authenticatorData),
      signature: encode(response.signature),
      // Who the authenticator says this is. Absent on a non-discoverable
      // credential, which this server does not use for signing in.
      userHandle: response.userHandle ? encode(response.userHandle) : undefined,
    },
    clientExtensionResults: credential.getClientExtensionResults(),
  };
}
