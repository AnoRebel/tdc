import type { User, UsernameAvailability, UsernamePolicy } from "@tdc/types";

/**
 * Changing what somebody is called: their display name freely, their username
 * under the server's rules.
 *
 * After either change the signed-in identity is read again rather than patched
 * here, so the address, the former addresses and everything derived from them
 * come from the server in one piece.
 */
export function useAccountNames() {
  const api = useApi();
  const { refresh } = useAuth();

  async function setDisplayName(displayName: string): Promise<User> {
    const updated = await api.put<User>("/api/me/display-name", { displayName });
    await refresh();
    return updated;
  }

  function usernamePolicy(): Promise<UsernamePolicy> {
    return api.get<UsernamePolicy>("/api/me/username");
  }

  function availability(username: string): Promise<UsernameAvailability> {
    return api.get<UsernameAvailability>(
      `/api/me/username/availability?username=${encodeURIComponent(username)}`,
    );
  }

  async function changeUsername(username: string, password: string): Promise<User> {
    const updated = await api.put<User>("/api/me/username", { username, password });
    await refresh();
    return updated;
  }

  return { setDisplayName, usernamePolicy, availability, changeUsername };
}
