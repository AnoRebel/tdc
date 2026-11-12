/**
 * Events: meetups, talks, workshops and office hours.
 *
 * Every instant is RFC 3339 in UTC. `timeZone` is the zone the organiser meant
 * them in, which is what a repeat is expanded in and what a client shows beside
 * the reader's own time when the two differ.
 */

export type EventKind = "meetup" | "talk" | "workshop" | "office_hours" | "other";
export type EventVisibility = "public" | "channel" | "invite";
export type EventAnswer = "going" | "maybe" | "declined" | "waitlisted";

export interface EventPerson {
  userId: string;
  username: string;
  displayName?: string;
  address: string;
}

/** An event as a list shows it; one entry per occurrence. */
export interface CalendarEvent {
  id: string;
  kind: EventKind;
  title: string;
  /** Markdown, in the syntax messages use. */
  description: string;
  /** The first occurrence. */
  startsAt: string;
  endsAt: string;
  timeZone: string;
  allDay: boolean;
  /** An RRULE body, e.g. "FREQ=WEEKLY;COUNT=6". */
  recurrence?: string;
  exceptions?: string[];
  location?: string;
  /** Hosted in this server's own call, in the event's conversation. */
  online: boolean;
  onlineUrl?: string;
  capacity?: number;
  visibility: EventVisibility;
  channel?: { id: string; name: string };
  cover?: { url: string; previewUrl?: string };
  cancelledAt?: string;
  cancelReason?: string;
  /** From a followed calendar, and so changed there. */
  imported?: boolean;
  myAnswer?: EventAnswer;
  /** In a list: which occurrence this entry is. */
  occurrenceStartsAt?: string;
  occurrenceEndsAt?: string;
}

export interface EventSession {
  id: string;
  title: string;
  description?: string;
  startsAt: string;
  endsAt: string;
  speakers: EventPerson[];
  guestSpeakers?: string;
}

/** One event with everything its page shows. */
export interface EventDetail extends CalendarEvent {
  hosts: EventPerson[];
  speakers: EventPerson[];
  sessions: EventSession[];
  counts: { going: number; maybe: number; waitlisted: number };
  waitlistPosition?: number;
  createdBy?: EventPerson;
  next?: { startsAt: string; endsAt: string };
  may: { edit: boolean; delete: boolean; startCall: boolean };
  /** In the event's conversation, and so able to join its call. */
  member: boolean;
  /** Its conversation silenced, and with it its reminders. */
  muted: boolean;
}

export interface EventSessionInput {
  title: string;
  description?: string;
  startsAt: string;
  endsAt: string;
  speakers: string[];
  guestSpeakers?: string;
}

/** What scheduling or changing an event sends. People are account ids. */
export interface EventInput {
  channelId?: string;
  kind: EventKind;
  title: string;
  description: string;
  startsAt: string;
  endsAt: string;
  timeZone: string;
  allDay: boolean;
  recurrence: string;
  exceptions: string[];
  location: string;
  online: boolean;
  onlineUrl: string;
  capacity: number | null;
  visibility: EventVisibility;
  hosts?: string[];
  speakers?: string[];
  sessions?: EventSessionInput[];
}

export interface EventAttendee extends EventPerson {
  status: EventAnswer;
}

export interface EventAttendance {
  attendees: EventAttendee[];
  /** People who keep their attendance to themselves, counted by answer. */
  unnamed: Partial<Record<EventAnswer, number>>;
}

export interface EventInvite {
  id: string;
  userId?: string;
  username?: string;
  /** The invited person's display name, absent when they have not set one. */
  displayName?: string;
  channelId?: string;
  channel?: string;
  createdAt: string;
}

export interface CalendarFeed {
  exists: boolean;
  createdAt?: string;
  lastUsedAt?: string;
  /** Only in the answer to making it: the secret is shown once. */
  path?: string;
}

export interface ImportPlan {
  uid: string;
  title: string;
  startsAt: string;
  endsAt: string;
  timeZone: string;
  allDay: boolean;
  recurrence?: string;
  location?: string;
  cancelled?: boolean;
  action: "create" | "update" | "skip";
  reason?: string;
  warnings: string[];
}

export interface CalendarSubscription {
  id: string;
  url: string;
  label: string;
  channelId?: string;
  visibility: "public" | "channel";
  kind: EventKind;
  lastFetchedAt?: string;
  lastError?: string;
  paused: boolean;
}

/** The live frame a reminder arrives as. */
export interface EventReminderFrame {
  event: string;
  title: string;
  startsAt: string;
  minutes: number;
  online: boolean;
}
