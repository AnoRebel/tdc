import { describe, expect, it } from "vitest";

import { subscribeFailure } from "./pushfailure";

/** The error a browser throws when its push service will not register it. */
function pushServiceError(): Error {
  const error = new Error("Registration failed - push service error");
  error.name = "AbortError";
  return error;
}

describe("what somebody is told when subscribing fails", () => {
  /*
   * The failure this exists for, found on a real browser.
   *
   * Chromium builds without Google's credentials always fail this way. The
   * browser's own words — "Registration failed - push service error" — name
   * neither the problem nor anything the reader can do, and somebody looking
   * at that on a settings page has no way to tell whether the fault is theirs,
   * the server's, or nobody's.
   */
  it("says a browser that cannot register is the limit", () => {
    const message = subscribeFailure(pushServiceError());

    expect(message).not.toBe("Registration failed - push service error");
    // Names where the limit is, so nobody goes looking through their own
    // settings or the server's for a fault that is in neither.
    expect(message).toContain("browser");
    // And says what still works, because notifications are not simply gone.
    expect(message.toLowerCase()).toContain("while it is open");
  });

  /*
   * An operator who rotated the VAPID pair leaves devices subscribed against
   * the old one. Worth distinguishing because the remedy is a step the reader
   * can take, rather than something they must wait for.
   */
  it("says how to recover from a key that no longer matches", () => {
    const error = new Error("Subscription failed");
    error.name = "InvalidStateError";

    const message = subscribeFailure(error);

    expect(message).toContain("Removing it");
  });

  /*
   * Anything uncharacterised keeps the browser's own words. A friendlier
   * message invented for a failure nobody has looked at would be a guess
   * presented as an explanation.
   */
  it("passes through a failure it does not recognise", () => {
    expect(subscribeFailure(new Error("Something specific went wrong"))).toBe(
      "Something specific went wrong",
    );
  });

  it("says something when the failure is not an error at all", () => {
    expect(subscribeFailure("a string")).toBe("This device could not be subscribed.");
    expect(subscribeFailure(undefined)).toBe("This device could not be subscribed.");
  });
});
