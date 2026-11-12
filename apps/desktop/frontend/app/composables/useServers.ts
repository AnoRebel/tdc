import * as Api from "#bindings/api";

/**
 * The servers this client knows about, and which one it is talking to.
 *
 * Only the desktop client has this. A browser is already at a server — the one
 * that served it — so there is nothing to choose; here the application is
 * installed once and may be pointed at a community server, an employer's, or
 * one somebody runs at home.
 *
 * The Go side holds the sessions, one per server. Switching is picking a
 * different one rather than signing in again, and this is the interface's view
 * of that.
 */

/** A server, as the interface shows it. */
export interface KnownServer {
  /** The checked origin, which is also its identity. */
  url: string;
  /** Whether this is the one requests currently go to. */
  current: boolean;
}

/** What went wrong when adding one, in words somebody can act on. */
export interface AddFailure {
  message: string;
}

export function useServers() {
  const servers = useState<KnownServer[]>("servers", () => []);
  const loading = useState("servers-loading", () => false);

  /** Reads the list back from Go, which is where the truth is. */
  async function refresh() {
    const [known, current] = await Promise.all([
      Api.Service.Servers(),
      Api.Service.CurrentServer(),
    ]);

    servers.value = (known ?? []).map((url) => ({ url, current: url === current }));
  }

  /**
   * Adds a server, or explains why not.
   *
   * The refusal comes from Go rather than being decided here: these bindings
   * are the only way in, and a check in the interface would be one a program
   * calling them could skip. What the interface does is show the reason.
   *
   * Returns the failure rather than throwing, because an address somebody
   * mistyped is something they can correct — not an error in the application.
   */
  async function add(address: string): Promise<AddFailure | null> {
    loading.value = true;

    try {
      /*
       * Reachability first, so somebody who mistyped a name is told that
       * rather than being sent to a sign-in form that fails for a reason it
       * cannot explain. A refused address never gets this far: Reachable
       * checks it before trying to connect.
       */
      await Api.Service.Reachable(address);
      await Api.Service.UseServer(address);
      await refresh();
      return null;
    } catch (error) {
      return { message: error instanceof Error ? error.message : String(error) };
    } finally {
      loading.value = false;
    }
  }

  /** Points requests at one already added. */
  async function use(url: string) {
    await Api.Service.UseServer(url);
    await refresh();
  }

  /** Removes one, and the session held for it. */
  async function forget(url: string) {
    await Api.Service.ForgetServer(url);
    await refresh();
  }

  return { servers, loading, refresh, add, use, forget };
}
