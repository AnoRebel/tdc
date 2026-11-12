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

  /*
   * Whether media marked sensitive should arrive covered.
   *
   * Covering while the server description is still loading, and if it failed
   * to load at all. The wrong answer in one direction briefly covers something
   * harmlessly; in the other it shows somebody exactly what was marked to
   * avoid showing them.
   */
  const obscureSensitiveMedia = computed(() => info.value?.obscureSensitiveMedia ?? true);

  // Loaded on first use rather than by every caller remembering to.
  void load();

  /*
   * How this server has chosen to look.
   *
   * Falls back to something usable while the description is still loading, so
   * the interface does not flash an empty badge on every start.
   */
  const branding = computed(
    () =>
      info.value?.branding ?? {
        name: "TDC",
        initials: "TDC",
      },
  );

  return { info, load, offers, obscureSensitiveMedia, branding };
}
