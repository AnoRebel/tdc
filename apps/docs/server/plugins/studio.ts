import { createHash } from "node:crypto";

/**
 * Refuses to start Studio with a session secret anybody could work out.
 *
 * Studio derives the key that seals its session cookie from the OAuth
 * credentials present when the site is *built*. Credentials are given to the
 * running container instead, so the build saw none and the key is the hash of
 * an empty string — the same on every deployment of this code. A signed-in
 * editor's session carries their GitHub token, so the key has to be secret:
 * NUXT_STUDIO_AUTH_SESSION_SECRET sets one at runtime, and with Studio
 * switched on and no such secret the server stops here rather than serve
 * sessions sealed with a public key.
 */
export default defineNitroPlugin(() => {
  if (!process.env.STUDIO_GITHUB_CLIENT_ID) return;

  const config = useRuntimeConfig();
  const secret = (config.studio as { auth?: { sessionSecret?: string } } | undefined)?.auth
    ?.sessionSecret;
  const derivedFromNothing = createHash("md5").update("").digest("hex");

  if (!secret || secret === derivedFromNothing || secret.length < 32) {
    throw new Error(
      "Studio is enabled (STUDIO_GITHUB_CLIENT_ID is set) but NUXT_STUDIO_AUTH_SESSION_SECRET " +
        "is missing or shorter than 32 characters. Set it to a random value, e.g. `openssl rand -hex 32`.",
    );
  }
});
