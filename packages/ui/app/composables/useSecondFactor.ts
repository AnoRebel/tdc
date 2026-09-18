import type { SecondFactorEnrolment, SecondFactorStatus } from "@tdc/types";

/**
 * A second thing somebody must have, beyond the one they know.
 *
 * Optional per account. An operator may require it, which the status below
 * reports so somebody is told why they are being asked rather than left to
 * wonder.
 */
export function useSecondFactor() {
  const api = useApi();

  /** Whether one is set up, whether it is required, and what is left. */
  async function status(): Promise<SecondFactorStatus> {
    return api.get<SecondFactorStatus>("/api/me/second-factor");
  }

  /**
   * Start setting up an authenticator app.
   *
   * Nothing is required at sign-in until it is confirmed, so a scan that
   * silently failed cannot lock somebody out of their own account.
   */
  async function begin(): Promise<SecondFactorEnrolment> {
    return api.post<SecondFactorEnrolment>("/api/me/second-factor/begin");
  }

  /**
   * Finish, by proving the app holds the secret.
   *
   * Returns the recovery codes, which come back here and nowhere else: only
   * their hashes are kept, so somebody who does not write them down now cannot
   * be shown them again — only given a new set.
   */
  async function confirm(code: string): Promise<string[]> {
    const result = await api.post<{ recoveryCodes: string[] }>("/api/me/second-factor/confirm", {
      code,
    });
    return result.recoveryCodes;
  }

  /** Replace the batch, for somebody who has used several or lost the paper. */
  async function reissueRecoveryCodes(): Promise<string[]> {
    const result = await api.post<{ recoveryCodes: string[] }>(
      "/api/me/second-factor/recovery-codes",
    );
    return result.recoveryCodes;
  }

  /**
   * Turn it off.
   *
   * The password again, because a session is not enough to remove a protection
   * — an unlocked laptop is a session. A POST rather than a DELETE because it
   * carries that password, and the shared transport gives DELETE no body.
   */
  async function disable(password: string): Promise<void> {
    await api.post("/api/me/second-factor/disable", { password });
  }

  return { status, begin, confirm, reissueRecoveryCodes, disable };
}
