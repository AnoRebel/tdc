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
  /** On the full view of one issue only. */
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

/** Refused because the issue changed first: here is what it became. */
export interface IssueConflict {
  message: string;
  issue: Issue;
}
