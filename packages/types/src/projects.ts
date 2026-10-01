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
