import type { IssueFromMessage } from "@tdc/types";
import { ClipboardPlus } from "lucide-vue-next";

import type { MenuItem } from "#layers/ui/app/components/ContextMenu.vue";

/**
 * Which of a conversation's messages became issues, and making another.
 *
 * Asked of the server per reader, never kept in the conversation itself: an
 * issue in a private project is listed only to the people who may see it, so
 * the room's other members are not told the project exists.
 */
export function useIssueLinks(roomId: Ref<string>) {
  const api = useApi();
  const { $ts } = useI18n();

  const links = ref<Record<string, IssueFromMessage[]>>({});

  /** The message somebody is turning into an issue, while the form is open. */
  const filing = ref<{ id: string; body: string } | null>(null);

  async function refresh() {
    if (!roomId.value) return;
    try {
      const issues = await api.get<IssueFromMessage[]>(`/api/rooms/${roomId.value}/issues`);
      const grouped: Record<string, IssueFromMessage[]> = {};
      for (const issue of issues) (grouped[issue.eventId] ??= []).push(issue);
      links.value = grouped;
    } catch {
      // Decoration on a conversation that has already loaded; missing it is
      // not worth an error over.
      links.value = {};
    }
  }

  watch(roomId, refresh, { immediate: true });

  function forMessage(eventId: string): IssueFromMessage[] {
    return links.value[eventId] ?? [];
  }

  /** The menu entry for a message, or nothing where it cannot apply. */
  function menuItem(message: { id: string; body: string; removed: boolean }): MenuItem | null {
    if (message.removed) return null;
    return {
      label: $ts("projects.fromMessage.action"),
      icon: ClipboardPlus,
      action: () => {
        filing.value = { id: message.id, body: message.body };
      },
    };
  }

  return { links, filing, refresh, forMessage, menuItem };
}
