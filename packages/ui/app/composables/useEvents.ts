import type {
  CalendarEvent,
  CalendarFeed,
  CalendarSubscription,
  EventAnswer,
  EventAttendance,
  EventDetail,
  EventInput,
  EventInvite,
  ImportPlan,
} from "@tdc/types";

/**
 * Events, through whichever transport this client has.
 *
 * Lists are of occurrences: a weekly meetup over a month is four entries, each
 * saying which occurrence it is.
 */
export function useEvents() {
  const api = useApi();
  const at = (id: string) => `/api/events/${encodeURIComponent(id)}`;

  return {
    list: (range: { from: string; to: string; channel?: string; limit?: number }) => {
      const query = new URLSearchParams({ from: range.from, to: range.to });
      if (range.channel) query.set("channel", range.channel);
      if (range.limit) query.set("limit", String(range.limit));
      return api.get<CalendarEvent[]>(`/api/events?${query}`);
    },
    get: (id: string) => api.get<EventDetail>(at(id)),
    create: (input: EventInput) => api.post<EventDetail>("/api/events", input),
    update: (id: string, input: EventInput) => api.put<EventDetail>(at(id), input),
    cancel: (id: string, reason: string) => api.post<EventDetail>(`${at(id)}/cancel`, { reason }),
    remove: (id: string) => api.delete<void>(at(id)),
    setCover: (id: string, attachmentId: string | null) =>
      api.put<EventDetail>(`${at(id)}/cover`, { attachmentId }),

    answer: (id: string, status: Exclude<EventAnswer, "waitlisted">) =>
      api.put<{ status: EventAnswer; position?: number }>(`${at(id)}/rsvp`, { status }),
    attendees: (id: string) => api.get<EventAttendance>(`${at(id)}/attendees`),
    invites: (id: string) => api.get<EventInvite[]>(`${at(id)}/invites`),
    invite: (id: string, people: { users?: string[]; channels?: string[] }) =>
      api.post<{ invited: number }>(`${at(id)}/invites`, people),
    uninvite: (id: string, inviteId: string) => api.delete<void>(`${at(id)}/invites/${inviteId}`),

    feed: () => api.get<CalendarFeed>("/api/me/calendar-feed"),
    makeFeed: () => api.post<CalendarFeed>("/api/me/calendar-feed"),
    revokeFeed: () => api.delete<void>("/api/me/calendar-feed"),

    previewImport: (ics: string) =>
      api.post<{ events: ImportPlan[] }>("/api/events/import/preview", { ics }),
    import: (choice: {
      ics: string;
      uids: string[];
      channelId?: string;
      visibility: string;
      kind: string;
    }) =>
      api.post<{ created: number; updated: number; skipped: number }>("/api/events/import", choice),

    subscriptions: () => api.get<CalendarSubscription[]>("/api/calendar/subscriptions"),
    subscribe: (details: {
      url: string;
      label: string;
      channelId?: string;
      visibility?: string;
      kind?: string;
    }) => api.post<CalendarSubscription>("/api/calendar/subscriptions", details),
    unsubscribe: (id: string) => api.delete<void>(`/api/calendar/subscriptions/${id}`),
    refreshSubscription: (id: string) =>
      api.post<{
        subscription: CalendarSubscription;
        created: number;
        updated: number;
        cancelled: number;
      }>(`/api/calendar/subscriptions/${id}/refresh`),
  };
}
