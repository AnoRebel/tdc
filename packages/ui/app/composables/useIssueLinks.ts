import type { IssueFromMessage } from "@tdc/types";
import { ClipboardPlus } from "lucide-vue-next";

import type { MenuItem } from "#layers/ui/app/components/ContextMenu.vue";

/**
 * Which of a conversation's messages became issues, and making another.
 *
 * Asked of the server per reader, never kept in the conversation itself: an
 * issue in a private project is listed only to the people who may see it, so
 * the room's other members are not told the project exists.
 *
 * Only in a client that has somewhere to show an issue. The desktop client
 * shares the conversation and has no issue pages yet, so there the list stays
 * empty and the menu offers nothing that would lead nowhere.
 */
export function useIssueLinks(roomId: Ref<string>) {
  const api = useApi();
  const router = useRouter();
  const { $ts } = useI18n();

  const available = router.hasRoute("issues-ref");
  const links = ref<Record<string, IssueFromMessage[]>>({});

  /** The message somebody is turning into an issue, while the form is open. */
  const filing = ref<{ id: string; body: string } | null>(null);

  async function refresh() {
    if (!available || !roomId.value) return;
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
    if (!available || message.removed) return null;
    return {
      label: $ts("projects.fromMessage.action"),
      icon: ClipboardPlus,
      action: () => {
        filing.value = { id: message.id, body: message.body };
      },
    };
  }

  return { available, links, filing, refresh, forMessage, menuItem };
}
