import type {
  CurrentPermissions,
  RegisterRequest,
  Session,
  SetupStatus,
  SignInRequest,
  SignInResult,
  User,
} from "@tdc/types";

/**
 * The signed-in user and what they may do.
 *
 * Held in Nuxt state rather than a module-level ref so that each request has
 * its own, and so the value survives navigation without a refetch.
 */
export function useAuth() {
  const api = useApi();

  const user = useState<User | null>("auth.user", () => null);
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
    } catch {
      // Not signed in, or the session has expired. Either way the interface
      // simply shows the signed-out state.
      user.value = null;
      permissions.value = [];
    } finally {
      ready.value = true;
    }
  }

  async function signIn(credentials: SignInRequest): Promise<void> {
    const result = await api.post<SignInResult>("/api/auth/signin", credentials);
    user.value = result.user;
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
