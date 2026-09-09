import type {
  CurrentPermissions,
  RegisterRequest,
  Session,
  SetupStatus,
  SignInRequest,
  SignInResult,
  User,
} from "@tdc/types";

import { readFailure } from "#layers/ui/app/lib/reachability";

/*
 * rememberedUserKey holds who was last signed in.
 *
 * Only what is needed to draw the interface before the server has been
 * reached. Not a credential: the session lives in an HttpOnly cookie no script
 * here can read, and the server checks every request regardless of what this
 * says.
 */
const rememberedUserKey = "tdc.user";

/**
 * The signed-in user and what they may do.
 *
 * Held in Nuxt state rather than a module-level ref so that each request has
 * its own, and so the value survives navigation without a refetch.
 */
export function useAuth() {
  const api = useApi();

  const user = useState<User | null>("auth.user", () => null);

  /*
   * The last identity the server confirmed.
   *
   * Kept so that opening the application with no connection shows the person
   * their conversations rather than a sign-in form. It is not a credential and
   * grants nothing: the session cookie is HttpOnly and every request is
   * checked by the server regardless. This only decides what to draw before
   * the server has been heard from.
   */
  function remembered(): User | null {
    if (import.meta.server) return null;

    try {
      const stored = localStorage.getItem(rememberedUserKey);
      return stored ? (JSON.parse(stored) as User) : null;
    } catch {
      return null;
    }
  }

  function remember(who: User | null) {
    if (import.meta.server) return;

    try {
      if (who) localStorage.setItem(rememberedUserKey, JSON.stringify(who));
      else localStorage.removeItem(rememberedUserKey);
    } catch {
      // A private window, or storage that is full. The application still
      // works; it simply asks the server again on every load.
    }
  }
  const permissions = useState<string[]>("auth.permissions", () => []);
  const ready = useState<boolean>("auth.ready", () => false);

  const signedIn = computed(() => user.value !== null);

  /**
   * Reports whether the user holds a permission.
   *
   * This decides only what to show. Every action is checked again on the
   * server, so a client that gets this wrong cannot grant itself anything.
   */
  function can(permission: string): boolean {
    return permissions.value.includes(permission);
  }

  /** Loads the current user, if the browser holds a valid session. */
  async function refresh(): Promise<void> {
    try {
      user.value = await api.get<User>("/api/me");
      const held = await api.get<CurrentPermissions>("/api/me/permissions");
      permissions.value = held.permissions;
      remember(user.value);
    } catch (cause) {
      /*
       * A server that could not be reached has not said anything about the
       * session, so nothing here is entitled to conclude it ended.
       *
       * Signing somebody out because their train went into a tunnel loses
       * their place, and — worse — the queued messages they composed while it
       * was down, because the sign-in page has no conversation to hold them.
       * What was last known stays until the server contradicts it.
       */
      if (readFailure(cause) === "unreachable") {
        // What was last known, so a reload with no connection is not read as
        // somebody having been signed out.
        user.value = remembered();
        return;
      }

      /*
        Not signed in, or the session has expired. Either way the server has
        said so, which is the only thing entitled to clear what was remembered.
      */
      user.value = null;
      permissions.value = [];
      remember(null);
    } finally {
      ready.value = true;
    }
  }

  async function signIn(credentials: SignInRequest): Promise<void> {
    const result = await api.post<SignInResult>("/api/auth/signin", credentials);
    user.value = result.user;
    remember(result.user);
    const held = await api.get<CurrentPermissions>("/api/me/permissions");
    permissions.value = held.permissions;
  }

  async function register(details: RegisterRequest): Promise<void> {
    await api.post<User>("/api/auth/register", details);
    await signIn({ username: details.username, password: details.password });
  }

  async function signOut(): Promise<void> {
    try {
      await api.post("/api/auth/signout");
    } finally {
      // Cleared even if the request fails: the session may already be gone,
      // and leaving the interface showing a signed-in state would be worse.
      // Somebody who signed out has said what they want, connection or not.
      remember(null);
      // Their conversations go with them: what was kept to read offline is
      // theirs, and the next person at this browser is not them.
      useLastSeen().forget();
      user.value = null;
      permissions.value = [];
    }
  }

  async function setupStatus(): Promise<SetupStatus> {
    return api.get<SetupStatus>("/api/setup");
  }

  async function completeSetup(details: RegisterRequest): Promise<void> {
    await api.post<User>("/api/setup", details);
    await signIn({ username: details.username, password: details.password });
  }

  async function listSessions(): Promise<Session[]> {
    return api.get<Session[]>("/api/me/sessions");
  }

  async function revokeSession(id: string): Promise<void> {
    await api.delete(`/api/me/sessions/${id}`);
  }

  return {
    user: readonly(user),
    permissions: readonly(permissions),
    ready: readonly(ready),
    signedIn,
    can,
    refresh,
    signIn,
    register,
    signOut,
    setupStatus,
    completeSetup,
    listSessions,
    revokeSession,
  };
}
