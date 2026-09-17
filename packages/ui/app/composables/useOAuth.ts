import type { OAuthResult, OAuthStart, SignInProvider } from "@tdc/types";

/*
 * The handle the callback left, and where it leaves it.
 *
 * In the fragment rather than the query string, because the server put it
 * there: a query string reaches browser history, the referer header and every
 * proxy in between, and what this handle stands for may be a session token.
 */
const handleParameter = "oauth";

/**
 * Signing in through a provider somebody else runs.
 *
 * Shared because both clients do the same three things — offer the providers,
 * send somebody to one, collect what came back — and differ only in how the
 * browser is opened. The web navigates; the desktop hands the URL to the
 * system browser and waits.
 */
export function useOAuth() {
  const api = useApi();
  const { info } = useServerInfo();

  /**
   * The providers this server offers.
   *
   * Empty on a server whose operator configured none, which is most of them,
   * and the sign-in form shows nothing extra in that case.
   */
  const providers = computed<SignInProvider[]>(() => info.value?.signInProviders ?? []);

  /** Whether there is anything to draw a button for. */
  const offered = computed(() => providers.value.length > 0);

  /**
   * Where to send somebody to sign in through a provider.
   *
   * next is a path inside this application to return to afterwards. The server
   * checks it before storing it, so what comes back is already safe to follow.
   */
  async function begin(provider: string, next?: string): Promise<OAuthStart> {
    const query = next ? `?next=${encodeURIComponent(next)}` : "";
    return api.get<OAuthStart>(`/api/auth/oauth/${encodeURIComponent(provider)}/start${query}`);
  }

  /**
   * The handle the callback left in the address, if this is a return from one.
   *
   * Read once and removed: leaving it in the address would mean a reload tries
   * to collect a handle that has already been spent, and the person would see
   * a failure for something that actually worked.
   */
  function pendingHandle(): string | null {
    if (import.meta.server) return null;

    const fragment = new URLSearchParams(window.location.hash.replace(/^#/, ""));
    const handle = fragment.get(handleParameter);
    if (!handle) return null;

    fragment.delete(handleParameter);
    const remaining = fragment.toString();
    window.history.replaceState(
      window.history.state,
      "",
      window.location.pathname + window.location.search + (remaining ? `#${remaining}` : ""),
    );

    return handle;
  }

  /**
   * Collect what the callback decided, and answer any question it asked.
   *
   * accept answers a link offer, and password is what proves the account it was
   * offered for. username and displayName name a new account. None of them
   * applies to an outcome that asked nothing, and sending them does no harm.
   */
  async function finish(request: {
    handle: string;
    accept?: boolean;
    password?: string;
    username?: string;
    displayName?: string;
  }): Promise<OAuthResult> {
    return api.post<OAuthResult>("/api/auth/oauth/finish", request);
  }

  return { providers, offered, begin, pendingHandle, finish };
}
