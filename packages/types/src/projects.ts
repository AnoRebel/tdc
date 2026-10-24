/**
 * Projects, the issues filed in them, and how they are tracked.
 *
 * Times are ISO 8601 instants with an offset. A date without a time — when a
 * milestone is due, when a cycle starts — is sent as the calendar date that
 * was chosen, in the project's own time zone, beside the instant it resolves
 * to; see {@link Milestone}.
 */

/** One of the states a project's issues move between. */
export interface IssueState {
  id: string;
  name: string;
  /** Order on a board, left to right. */
  position: number;
  /** Whether an issue here counts as finished. */
  isCompletion: boolean;
}

/**
 * What the requester may do in a project.
 *
 * A courtesy for deciding what to offer: the server checks again on every
 * request regardless.
 */
export interface ProjectAbilities {
  administer: boolean;
  configureStates: boolean;
  createIssues: boolean;
  transition: boolean;
  assign: boolean;
  label: boolean;
  relate: boolean;
  delete: boolean;
  /** Making cycles and milestones. */
  plan: boolean;
  /** Putting issues into cycles and milestones. */
  planIssues: boolean;
}

/** One of a project's labels. */
export interface IssueLabel {
  id: string;
  name: string;
  /** #rrggbb. */
  colour: string;
}

/** One level of a project's priority scale. */
export interface IssuePriority {
  id: string;
  name: string;
  /** Its place on the scale, 1 being the most urgent. */
  rank: number;
}

/** Somebody, as an issue names them. */
export interface PersonRef {
  username: string;
  displayName: string;
  address: string;
}

/** Another issue, as a relationship shows it. */
export interface RelatedIssue {
  key: string;
  title: string;
  state: string;
  completed: boolean;
}

/** How an issue stands among the others in its project. */
export interface IssueRelations {
  parent?: RelatedIssue;
  children: RelatedIssue[];
  blocks: RelatedIssue[];
  blockedBy: RelatedIssue[];
  /** How many of its children are finished, from their states as they are now. */
  childProgress: { completed: number; total: number };
}

/** What one issue is to another, from its own side. */
export type RelationKind = "parent" | "child" | "blocks" | "blocked_by";

export interface Project {
  id: string;
  /** What every identifier in it starts with: WEB in WEB-12. Never changes. */
  key: string;
  name: string;
  description: string;
  visibility: "public" | "private";
  createdAt: string;
  /** The requester's role, absent when they can see it without being in it. */
  role?: "admin" | "member";
  /** Present on a single project, in board order. */
  states?: IssueState[];
  can?: ProjectAbilities;
  /** Present on a single project. */
  labels?: IssueLabel[];
  /** Present on a single project, most urgent first. */
  priorities?: IssuePriority[];
  /** The calendar its dates are on: an IANA zone, UTC unless set. */
  timeZone: string;
  /** What happens to unfinished issues when a cycle ends. */
  cyclePolicy: CyclePolicy;
}

export interface ProjectMember {
  username: string;
  displayName: string;
  address: string;
  role: "admin" | "member";
  joinedAt: string;
}

/** What the requester may do to one issue. */
export interface IssueAbilities {
  edit: boolean;
  transition: boolean;
  assign: boolean;
  label: boolean;
  relate: boolean;
  delete: boolean;
  plan: boolean;
}

/**
 * The conversation an issue was made from, as far as the reader may know.
 *
 * Two shapes only. Somebody who can read the room sees it and the message as
 * it reads there now; anybody else sees `{ disclosed: false }` and nothing
 * else — not the room's name, and not whether the message still exists.
 */
export interface IssueOrigin {
  disclosed: boolean;
  roomId?: string;
  roomName?: string;
  eventId?: string;
  message?: {
    sender: string;
    sentAt: string;
    body?: string;
    edited?: boolean;
    removed?: boolean;
  };
}

export interface Issue {
  id: string;
  /** The identifier people use: WEB-12. It never changes. */
  key: string;
  number: number;
  title: string;
  state: IssueState;
  /** Who filed it, as user@server. */
  author?: string;
  createdAt: string;
  updatedAt: string;
  assignees: PersonRef[];
  labels: IssueLabel[];
  priority?: IssuePriority;
  /** The cycle it is in, if any. */
  cycle?: PlanRef;
  /** The milestone it counts towards, if any. */
  milestone?: PlanRef;
  /** On the full view of one issue only. */
  relations?: IssueRelations;
  description?: string;
  project?: Project;
  can?: IssueAbilities;
  origin?: IssueOrigin;
}

/** One recorded change to an issue, from its project's event log. */
export interface IssueHistoryEntry {
  id: string;
  /** issue.created, issue.state_changed, and so on. */
  type: string;
  /** Who made the change, as user@server. */
  actor: string;
  at: string;
  sequence: number;
  /**
   * What was recorded. A change to a field carries field, previous and new;
   * a move between states also carries previousName and newName.
   */
  content: Record<string, unknown>;
}

/**
 * One recorded change to a project itself — project.created,
 * project.label_added and the rest — in the same shape as an issue's. The
 * actor is the server's own name, with no "@", for what it did by itself.
 */
export type ProjectHistoryEntry = IssueHistoryEntry;

/**
 * An issue made from one of a room's messages, as that room's reader may see
 * it. Issues in projects the reader cannot see are simply not listed.
 */
export interface IssueFromMessage {
  /** The message it was made from. */
  eventId: string;
  key: string;
  title: string;
  /** The name of the state it is in. */
  state: string;
  /** Whether that state means done. */
  completed: boolean;
}

/** An issue that matched a search. */
export interface IssueSearchResult {
  id: string;
  key: string;
  title: string;
  projectKey: string;
  projectName: string;
  state: string;
  completed: boolean;
  updatedAt: string;
  /** The title and description with the matched words in `<mark>`, when there were words. */
  highlighted?: string;
}

/** Something said in an issue's discussion that matched a search. */
export interface DiscussionSearchResult {
  eventId: string;
  sender: string;
  sentAt: string;
  issueKey: string;
  issueTitle: string;
  highlighted: string;
}

/** What an issue search found: issues, and what was said about them. */
export interface IssueSearch {
  issues: IssueSearchResult[];
  discussions: DiscussionSearchResult[];
}

/** An issue assigned to the requester, with the project it is in. */
export interface MyIssue extends Issue {
  projectKey: string;
  projectName: string;
}

/** Refused because the issue changed first: here is what it became. */
export interface IssueConflict {
  message: string;
  issue: Issue;
}

/** An issue as a board card shows it, and nothing more. */
export interface BoardCard {
  key: string;
  title: string;
  priority?: { name: string; rank: number };
  labels?: { name: string; colour: string }[];
  /** Usernames. */
  assignees?: string[];
}

/** One state of a board, and a page of the issues in it. */
export interface BoardColumn {
  stateId: string;
  name: string;
  isCompletion: boolean;
  /** How many issues the state holds, all of them. */
  total: number;
  cards: BoardCard[];
  /** Continues the column; absent on its last page. */
  next?: string;
}

/** A project's issues as columns, one per state, in board order. */
export interface Board {
  columns: BoardColumn[];
}

/** What a board shows: label and priority by name, assignee by username. */
export interface BoardFilter {
  label?: string;
  priority?: string;
  assignee?: string;
  /**
   * A cycle by id, "none" for issues in no cycle, or "current" for whichever
   * cycle is under way when the board is asked for — what a saved view of
   * "this cycle's board" keeps.
   */
  cycle?: string;
}

/**
 * How a saved view asks: the same names a search takes. A view keeps the
 * question, never the answer, so whoever opens it sees what they may see.
 */
export interface ViewFilter extends BoardFilter {
  q?: string;
  /** A state's name. */
  state?: string;
  sort?: "newest" | "priority";
}

/** A saved way of looking at issues. */
export interface SavedView {
  id: string;
  /** The key of the project it views; absent for a view across projects. */
  project?: string;
  name: string;
  layout: "list" | "board";
  filter: ViewFilter;
  shared: boolean;
  /** The username of whoever saved it. */
  owner: string;
  /** Whether the requester saved it, and so may change it. */
  mine: boolean;
  updatedAt: string;
}

/** A view opened by the requester: what it asks, and what it found for them. */
export interface OpenedView {
  view: SavedView;
  issues?: IssueSearchResult[];
  board?: Board;
}

/** One change to make to many issues of a project. */
export interface BulkChange {
  kind: "state" | "assign" | "unassign" | "label" | "unlabel" | "priority" | "cycle";
  /** A state, label, priority level or cycle by id, or a person by username. */
  value: string;
}

/** What became of one issue in a batch. */
export interface BulkResult {
  issue: string;
  code: "changed" | "unchanged" | "refused" | "not_found" | "failed";
  changed: boolean;
  reason?: string;
  /** For a refusal, the permission that would have allowed it. */
  permission?: string;
}

/** What happens to unfinished issues when a cycle ends. */
export type CyclePolicy = "carry-forward" | "remain";

/** A cycle or milestone, as an issue names it. */
export interface PlanRef {
  id: string;
  name: string;
}

/**
 * A cycle: a span of days in the project's calendar.
 *
 * `startsOn` and `endsOn` are days — "2026-10-18" — to show as they are, never
 * through a Date, which would read them as midnight UTC and show the 17th to
 * anybody west of Greenwich. `startsAt` and `endsAt` are those days' first
 * instant and the first instant after, in the project's zone, with its offset.
 */
export interface Cycle {
  id: string;
  name: string;
  startsOn: string;
  endsOn: string;
  startsAt: string;
  endsAt: string;
  status: "upcoming" | "active" | "ended";
  total: number;
  completed: number;
  /** When its end was dealt with, and under which policy. */
  closedAt?: string;
  closedPolicy?: CyclePolicy;
}

/** A milestone: issues across cycles, with an advisory day to reach it by. */
export interface Milestone {
  id: string;
  name: string;
  description: string;
  /** A day, like a cycle's. */
  targetDate?: string;
  /** The instant that day is over, in the project's zone. */
  dueAt?: string;
  /** Its day has passed with work unfinished. Nothing else changes because of it. */
  overdue: boolean;
  total: number;
  completed: number;
}

/** How things stood at the end of one day in the project's calendar, or now for today. */
export interface ProgressPoint {
  /** A day, "YYYY-MM-DD". */
  day: string;
  total: number;
  completed: number;
}

/**
 * How much of a project, cycle or milestone is done.
 *
 * `total` and `completed` are counted from the issues as they are; `trend` is
 * replayed from the project's history, one point a day. Neither is stored, so
 * today's point and the figures always agree.
 */
export interface Progress {
  total: number;
  completed: number;
  from: string;
  to: string;
  trend: ProgressPoint[];
}
