import type { ApiRequest, ApiResponse } from "@tdc/types";

/**
 * How the desktop client reaches the server: over Wails bindings, not HTTP.
 *
 * The only thing the two clients disagree about. Everything above this — every
 * composable in the shared layer, and every screen built on them — is written
 * against the transport contract and does not know which is underneath.
 *
 * Separated from the plugin that installs it so it can be tested without a
 * Nuxt runtime. The contract both adapters are held to runs against this
 * function directly; a transport that could only be exercised inside a
 * framework would be one that never got held to the same standard as the web
 * one.
 */

/** What the generated binding is, as far as this needs to know. */
export interface Binding {
  Send(request: { method: string; path: string; body?: unknown }): Promise<{
    status?: number;
    body?: unknown;
    ok: boolean;
    retryAfterSeconds?: number;
  }>;
}

/**
 * Thrown when nothing answered at all.
 *
 * Carries no status, deliberately: `readFailure` in the shared layer tells a
 * refusal from an unreachable server by whether a status is present, and the
 * web adapter rethrows fetch's own status-less TypeError for the same case.
 * Both clients therefore report unreachability the same way — which is the
 * point, since the code deciding what to do about it is shared.
 */
export class Unreachable extends Error {
  constructor(cause: unknown) {
    super(messageFor(cause), { cause });
    this.name = "Unreachable";
  }
}

/** What the Go side said, where it said anything useful. */
function messageFor(cause: unknown): string {
  const message = cause instanceof Error ? cause.message : String(cause ?? "");

  /*
   * Its own words where there are any. Go reports which address it could not
   * reach, which is the difference between "something went wrong" and knowing
   * the server has moved or the machine is asleep.
   */
  return message || "The server could not be reached.";
}

/**
 * Builds the transport over a binding.
 *
 * The binding is passed in rather than imported so this can be exercised
 * against a stand-in: the generated one talks to a native host that does not
 * exist in a test process, and a transport that could only be tested with the
 * application running would not be tested.
 */
export function createWailsTransport(binding: Binding) {
  return async function send(request: ApiRequest): Promise<ApiResponse> {
    let answer;

    try {
      answer = await binding.Send({
        method: request.method,
        path: request.path,
        /*
         * Handed over as it is. The contract keeps a body unknown on purpose
         * — Wails marshals an object itself, so encoding it here would mean
         * the Go side parsing a string this had just produced.
         */
        body: request.body,
      });
    } catch (cause) {
      /*
       * No answer at all: the machine is unreachable, the name does not
       * resolve, the connection died. A refusal by the server is not this —
       * Go returns those as an answer with ok false, because a 403 is
       * something the server said and the interface needs its body to say
       * why.
       */
      throw new Unreachable(cause);
    }

    return {
      ok: answer.ok,
      status: answer.status,
      body: answer.body,
      retryAfterSeconds: answer.retryAfterSeconds,
    };
  };
}
