import type { DigestFrequency, DigestSetting } from "@tdc/types";

/**
 * A periodic summary of missed activity, by email, instead of interruptions.
 *
 * Offered only where the server can send mail: it is absent from the server's
 * capabilities otherwise, and both routes answer 404. A control for something
 * nobody can deliver is worse than no control.
 */
export function useDigests() {
  const api = useApi();
  const { offers } = useServerInfo();

  /** Whether this server can send summaries at all. */
  const available = computed(() => offers("notification-digests"));

  async function read(): Promise<DigestSetting> {
    return api.get<DigestSetting>("/api/me/preferences/digest");
  }

  /**
   * Choose how often, and when.
   *
   * The zone is sent with every change, because the hour means nothing without
   * it: eight o'clock in Dar es Salaam and eight o'clock in London are three
   * hours apart.
   */
  async function choose(choice: {
    frequency: DigestFrequency;
    hour: number;
    weekday: number;
    timeZone: string;
  }): Promise<DigestSetting> {
    return api.put<DigestSetting>("/api/me/preferences/digest", choice);
  }

  return { available, read, choose };
}

/**
 * The zone this device is in, as an IANA name.
 *
 * What a first choice defaults to, since it is almost always where the person
 * is. UTC where the runtime will not say, which the server accepts and the
 * control then shows plainly.
 */
export function deviceTimeZone(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";
  } catch {
    return "UTC";
  }
}
