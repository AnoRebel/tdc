import type { BlockedPerson, ChannelBan, DirectMessagePolicy, Report } from "@tdc/types";

/**
 * Blocking and privacy, which belong to the person rather than to a moderator.
 *
 * Blocking is never disclosed to the person blocked: nothing here is shown to
 * them, and the refusal they receive when they try to open a conversation says
 * only that it cannot be opened.
 */
export function usePersonalModeration() {
  const api = useApi();

  const blocked = ref<BlockedPerson[]>([]);
  const directMessages = ref<DirectMessagePolicy>("anyone");

  async function refresh() {
    try {
      const [people, privacy] = await Promise.all([
        api.get<BlockedPerson[]>("/api/me/blocks"),
        api.get<{ directMessages: DirectMessagePolicy }>("/api/me/privacy"),
      ]);
      blocked.value = people;
      directMessages.value = privacy.directMessages;
    } catch {
      // A settings page that cannot load is shown empty rather than as an
      // error: the rest of the application still works.
    }
  }

  async function block(userId: string) {
    await api.post("/api/me/blocks", { user: userId });
    await refresh();
  }

  async function unblock(userId: string) {
    await api.delete(`/api/me/blocks/${userId}`);
    await refresh();
  }

  async function setDirectMessages(policy: DirectMessagePolicy) {
    const before = directMessages.value;
    directMessages.value = policy;

    try {
      await api.put("/api/me/privacy", { directMessages: policy });
    } catch {
      // Put back, so the control never shows a setting the server did not
      // accept.
      directMessages.value = before;
    }
  }

  return { blocked, directMessages, refresh, block, unblock, setDirectMessages };
}

/**
 * Channel bans, which are a moderator's to issue.
 *
 * Confined to the channel they name: somebody banned from one keeps every
 * other membership, which is what separates a ban from a suspension.
 */
export function useChannelBans(roomId: Ref<string>) {
  const api = useApi();

  const bans = ref<ChannelBan[]>([]);
  const failed = ref(false);

  async function refresh() {
    try {
      bans.value = await api.get<ChannelBan[]>(`/api/rooms/${roomId.value}/bans`);
      failed.value = false;
    } catch {
      // Most people cannot read this: the refusal is expected rather than
      // exceptional, so the list simply stays empty.
      bans.value = [];
      failed.value = true;
    }
  }

  /** Bans somebody, optionally for a fixed number of hours. */
  async function ban(userId: string, reason: string, hours?: number) {
    await api.post(`/api/rooms/${roomId.value}/bans`, { user: userId, reason, hours });
    await refresh();
  }

  async function lift(userId: string) {
    await api.delete(`/api/rooms/${roomId.value}/bans/${userId}`);
    await refresh();
  }

  return { bans, failed, refresh, ban, lift };
}

/**
 * The moderation queue.
 *
 * Reading this needs the review permission, so most people never see it. The
 * reporter's identity is in what comes back, and it is here — behind that
 * permission — that it appears at all: the reported party has no endpoint that
 * returns it, and nothing they can read names who complained.
 */
export function useReportQueue() {
  const api = useApi();

  const reports = ref<Report[]>([]);
  const open = ref(0);
  const failed = ref(false);
  const loading = ref(false);

  async function refresh() {
    loading.value = true;
    try {
      reports.value = await api.get<Report[]>("/api/reports?limit=100");
      open.value = reports.value.length;
      failed.value = false;
    } catch {
      reports.value = [];
      failed.value = true;
    } finally {
      loading.value = false;
    }
  }

  /**
   * Counts what is waiting without reading it.
   *
   * The badge is refreshed often; the queue itself is not, and fetching every
   * report to show a number would pull reported text into memory on a screen
   * that only ever displays a count.
   */
  async function count() {
    try {
      const result = await api.get<{ open: number }>("/api/reports/count");
      open.value = result.open;
      failed.value = false;
    } catch {
      failed.value = true;
    }
  }

  /*
   * Resolving and dismissing are the same call with a different outcome
   * recorded, and both leave the queue. Kept as two functions because a
   * boolean argument at the call site reads as neither.
   */

  async function resolve(id: string, resolution: string) {
    await api.post(`/api/reports/${id}/resolve`, { dismissed: false, resolution });
    await refresh();
  }

  async function dismiss(id: string, resolution: string) {
    await api.post(`/api/reports/${id}/resolve`, { dismissed: true, resolution });
    await refresh();
  }

  return { reports, open, failed, loading, refresh, count, resolve, dismiss };
}
