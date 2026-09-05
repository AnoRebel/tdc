import type { Capability, ServerInfo } from "@tdc/types";

/**
 * What this server is and what it offers.
 *
 * Fetched once and shared, because capabilities do not change while somebody
 * is using the application: an operator switching a feature on restarts the
 * server, which ends every session anyway.
 *
 * Held at module scope rather than in the composable, so ten components asking
 * make one request rather than ten.
 */
const info = ref<ServerInfo | null>(null);
let inFlight: Promise<void> | null = null;

export function useServerInfo() {
  const api = useApi();

  async function load() {
    if (info.value || inFlight) return inFlight ?? Promise.resolve();

    inFlight = api
      .get<ServerInfo>("/api/server")
      .then((loaded) => {
        info.value = loaded;
      })
      .catch(() => {
        // A server that cannot describe itself is treated as offering nothing
        // optional. Every optional feature has a path that works without it,
        // so the application still runs.
      })
      .finally(() => {
        inFlight = null;
      });

    return inFlight;
  }

  /** Whether the server offers an optional capability. */
  function offers(capability: Capability): boolean {
    return info.value?.capabilities?.includes(capability) ?? false;
  }

  // Loaded on first use rather than by every caller remembering to.
  void load();

  return { info, load, offers };
}
