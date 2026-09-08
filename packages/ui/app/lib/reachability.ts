/**
 * Telling a server that refused from a server that was never there.
 *
 * The distinction matters in several places and means the same thing in all of
 * them: a refusal is the server's answer and will not change, and anything
 * else is the network, which might. Getting it backwards either discards work
 * somebody meant to keep or retries something that will never be accepted.
 */

/** Why something failed, as far as anything here needs to know. */
export type Failure = "refused" | "unreachable";

/**
 * Decides which kind of failure this was.
 *
 * A refusal carries a status. fetch rejects with a TypeError when it could not
 * reach the server at all, which has none — so the presence of a status is
 * what separates "the server said no" from "there was no server".
 */
export function readFailure(cause: unknown): Failure {
  if (typeof cause === "object" && cause !== null && "status" in cause) {
    const status = (cause as { status: unknown }).status;

    /*
     * A 5xx is the server failing rather than refusing. It read the request
     * and could not complete it — a restart, a database blip, a deploy — and
     * the same request a minute later may well succeed.
     */
    if (typeof status === "number" && status >= 400 && status < 500) return "refused";
  }

  return "unreachable";
}
