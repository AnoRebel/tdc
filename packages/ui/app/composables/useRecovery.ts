import type { EmailState, RecoveryRedeemed, RecoveryRequested } from "@tdc/types";

/**
 * Getting back into an account whose password is gone.
 *
 * Offered only where the server can send mail. A link on a server that cannot
 * is worse than no link, because it promises something nobody can deliver.
 */
export function useRecovery() {
  const api = useApi();
  const { info } = useServerInfo();

  /** Whether this server can mail somebody a way back in. */
  const available = computed(() => info.value?.recovery === true);

  /**
   * Ask for a link.
   *
   * The answer is the same whether or not there is an account with that
   * address, and so is what a client should show. Anything that branched on
   * the response would be re-introducing the disclosure the server refuses.
   */
  async function request(email: string): Promise<RecoveryRequested> {
    return api.post<RecoveryRequested>("/api/auth/recover", { email });
  }

  /**
   * Whether a link is still worth showing a form for.
   *
   * Asked before the form rather than after, so somebody following an expired
   * link is told before they choose a password twice.
   */
  async function usable(token: string): Promise<boolean> {
    try {
      await api.get<{ usable: boolean }>(`/api/auth/recover?token=${encodeURIComponent(token)}`);
      return true;
    } catch {
      // Unknown, spent or expired. One answer, as the server gives one.
      return false;
    }
  }

  /**
   * Use a link and set a new password.
   *
   * No session comes back: signing in afterwards is what proves the password
   * is known and goes through the second factor where there is one.
   */
  async function redeem(token: string, password: string): Promise<RecoveryRedeemed> {
    return api.post<RecoveryRedeemed>("/api/auth/recover/redeem", { token, password });
  }

  /** Somebody's own address, and whether it has been proved. */
  async function emailState(): Promise<EmailState> {
    return api.get<EmailState>("/api/me/email");
  }

  /**
   * Ask to prove an address.
   *
   * The account keeps whatever address it already had until the link is used,
   * so a mistyped one costs nothing but the message.
   */
  async function confirmAddress(email: string): Promise<{ message: string }> {
    return api.post<{ message: string }>("/api/me/email", { email });
  }

  /** Use a confirmation link. */
  async function completeConfirmation(token: string): Promise<{ message: string }> {
    return api.post<{ message: string }>("/api/auth/confirm-email", { token });
  }

  return {
    available,
    request,
    usable,
    redeem,
    emailState,
    confirmAddress,
    completeConfirmation,
  };
}
