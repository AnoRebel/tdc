import type { OwnStatus, PrivacySettings, ServerStatus } from "@tdc/types";

/**
 * The signed-in person's own status, and how they appear to others.
 *
 * The statuses are the operator's — whatever this server lists — so nothing
 * here assumes "busy" exists. The one meaning taken from the list is the
 * status keyed "offline", which every server is created with: choosing it also
 * makes the person appear offline while connected, which is the server's
 * separate switch for that. Otherwise others would see somebody online whose
 * status reads "Appear offline", which is the one thing that status must not
 * say.
 */
export const appearOfflineKey = "offline";

export function useOwnStatus() {
  const api = useApi();

  const own = useState<OwnStatus>("own-status", () => ({
    status: "available",
    statusText: "",
    invisible: false,
  }));
  const statuses = useState<ServerStatus[]>("server-statuses", () => []);

  async function refresh() {
    try {
      const [mine, available] = await Promise.all([
        api.get<OwnStatus>("/api/me/status"),
        statuses.value.length
          ? Promise.resolve(statuses.value)
          : api.get<ServerStatus[]>("/api/statuses"),
      ]);
      own.value = mine;
      statuses.value = available;
    } catch {
      // Kept as it was: the footer goes on showing the last known status.
    }
  }

  /**
   * Chooses a status, optionally with a note and a time it clears itself.
   *
   * Shown at once and put back if the server refuses. The server answers with
   * what it recorded — including the exact moment it lapses — which replaces
   * the guess.
   */
  async function set(choice: { status: string; text?: string; expiresInSeconds?: number }) {
    const before = own.value;
    const invisible = choice.status === appearOfflineKey;
    own.value = {
      status: choice.status,
      statusText: choice.text ?? "",
      invisible,
      expiresAt: choice.expiresInSeconds
        ? new Date(Date.now() + choice.expiresInSeconds * 1000).toISOString()
        : undefined,
    };

    try {
      own.value = await api.put<OwnStatus>("/api/me/status", {
        status: choice.status,
        text: choice.text ?? "",
        expiresInSeconds: choice.expiresInSeconds ?? 0,
        invisible,
      });
    } catch (cause) {
      own.value = before;
      throw cause;
    }
  }

  /** Back to the default, with no note and nothing to lapse. */
  function clear() {
    return set({ status: "available" });
  }

  /** The operator's label for a status key, or the key itself. */
  function labelOf(key: string): string {
    return statuses.value.find((status) => status.key === key)?.label ?? key;
  }

  return { own, statuses, refresh, set, clear, labelOf };
}

/**
 * Whether others are shown that this person is writing.
 *
 * Read once and shared, because the composer asks on every keystroke and the
 * settings screen is where it changes. Turned off, the composer stops
 * announcing — and the server ignores an announcement anyway, so an older
 * client cannot give somebody away.
 */
export function useTypingPreference() {
  const api = useApi();
  const sends = useState<boolean | null>("sends-typing", () => null);

  async function load() {
    if (sends.value !== null) return;
    try {
      const privacy = await api.get<PrivacySettings>("/api/me/privacy");
      sends.value = privacy.sendsTypingIndication ?? true;
    } catch {
      // Unknown: the composer announces, and the server applies the setting.
    }
  }

  async function set(value: boolean) {
    const before = sends.value;
    sends.value = value;
    try {
      await api.put("/api/me/privacy", { sendsTypingIndication: value });
    } catch (cause) {
      sends.value = before;
      throw cause;
    }
  }

  return { sends, load, set };
}
