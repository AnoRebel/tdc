import type { AutomaticReply, AutomaticReplyInput } from "@tdc/types";

/**
 * The signed-in person's automatic replies: what the server says on their
 * behalf while they are away.
 *
 * Shared state, because the settings section and the status dialog both change
 * them, and each should see what the other did. After every change the own
 * status is refreshed too, since it carries whether a reply is on now — which
 * is what the foot of the sidebar shows.
 */
export function useAutomaticReplies() {
  const api = useApi();
  const status = useOwnStatus();
  const replies = useState<AutomaticReply[] | null>("automatic-replies", () => null);

  async function load() {
    replies.value = await api.get<AutomaticReply[]>("/api/me/automatic-replies");
  }

  async function settle() {
    await Promise.all([load(), status.refresh()]);
  }

  async function create(input: AutomaticReplyInput) {
    const created = await api.post<AutomaticReply>("/api/me/automatic-replies", input);
    await settle();
    return created;
  }

  async function update(id: string, input: AutomaticReplyInput) {
    const updated = await api.put<AutomaticReply>(`/api/me/automatic-replies/${id}`, input);
    await settle();
    return updated;
  }

  async function remove(id: string) {
    await api.delete(`/api/me/automatic-replies/${id}`);
    await settle();
  }

  /**
   * The reply the status dialog manages for one status: tied to that status
   * alone, with no window. Anything more particular was made in settings and
   * is left for settings to change.
   */
  function forStatus(key: string): AutomaticReply | undefined {
    return replies.value?.find(
      (reply) =>
        reply.statuses.length === 1 &&
        reply.statuses[0] === key &&
        !reply.startsAt &&
        !reply.endsAt,
    );
  }

  return { replies, load, create, update, remove, forStatus };
}

/** The longest an automatic reply may be, as the server holds it. */
export const automaticReplyLimit = 500;
