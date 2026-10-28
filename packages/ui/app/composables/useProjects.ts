import type {
  Board,
  BoardColumn,
  BoardFilter,
  BulkChange,
  BulkResult,
  Cycle,
  CyclePolicy,
  Milestone,
  OpenedView,
  Progress,
  SavedView,
  ViewFilter,
  DiscussionSearchResult,
  Issue,
  IssueHistoryEntry,
  ProjectHistoryEntry,
  IssueSearch,
  IssueLabel,
  IssuePriority,
  IssueState,
  MyIssue,
  Project,
  ProjectMember,
  RelationKind,
} from "@tdc/types";

/**
 * Projects and their issues, through whichever transport this client has.
 *
 * Paths name issues by their identifier — WEB-12 — rather than by an internal
 * id, because that is the name people use and the one that never changes.
 */
export function useProjects() {
  const api = useApi();
  const at = (key: string) => `/api/projects/${encodeURIComponent(key)}`;
  const issueAt = (ref: string) => `/api/issues/${encodeURIComponent(ref)}`;

  return {
    list: () => api.get<Project[]>("/api/projects"),
    get: (key: string) => api.get<Project>(at(key)),
    create: (details: {
      key: string;
      name: string;
      description?: string;
      visibility: "public" | "private";
    }) => api.post<Project>("/api/projects", details),
    update: (
      key: string,
      changes: { name?: string; description?: string; visibility?: "public" | "private" },
    ) => api.patch<Project>(at(key), changes),

    members: (key: string) => api.get<ProjectMember[]>(`${at(key)}/members`),
    /** What happened to the project itself, newest first; before continues below a sequence. */
    projectHistory: (key: string, before?: number) =>
      api.get<ProjectHistoryEntry[]>(`${at(key)}/history${before ? `?before=${before}` : ""}`),
    setMember: (key: string, username: string, role: "admin" | "member") =>
      api.put<void>(`${at(key)}/members/${encodeURIComponent(username)}`, { role }),
    removeMember: (key: string, username: string) =>
      api.delete<void>(`${at(key)}/members/${encodeURIComponent(username)}`),
    setFederated: (key: string, federated: boolean) =>
      api.put<Project>(`${at(key)}/federation`, { federated }),

    addState: (key: string, state: { name: string; isCompletion: boolean }) =>
      api.post<IssueState>(`${at(key)}/states`, state),
    updateState: (key: string, id: string, changes: { name?: string; isCompletion?: boolean }) =>
      api.patch<IssueState>(`${at(key)}/states/${id}`, changes),
    reorderStates: (key: string, order: string[]) =>
      api.put<IssueState[]>(`${at(key)}/states`, { order }),
    /** Removes a state; issues still in it go to moveTo. */
    removeState: (key: string, id: string, moveTo?: string) =>
      api.delete<void>(
        `${at(key)}/states/${id}${moveTo ? `?moveTo=${encodeURIComponent(moveTo)}` : ""}`,
      ),

    /** A page of a project's issues; every filter given must match. */
    issues: (
      key: string,
      options: {
        state?: string;
        label?: string;
        priority?: string;
        assignee?: string;
        sort?: "newest" | "priority";
        before?: number;
        limit?: number;
      } = {},
    ) => {
      const query = new URLSearchParams();
      for (const [name, value] of Object.entries(options)) {
        if (value !== undefined && value !== "") query.set(name, String(value));
      }
      const suffix = query.size ? `?${query}` : "";
      return api.get<Issue[]>(`${at(key)}/issues${suffix}`);
    },
    mine: () => api.get<MyIssue[]>("/api/me/issues"),
    /**
     * Searches issues across every project the requester can see. Words and
     * filters narrow together; filters name states, labels and priorities
     * rather than identifying them, so they mean the same in every project.
     */
    search: (query: {
      q?: string;
      project?: string;
      state?: string;
      label?: string;
      priority?: string;
      assignee?: string;
      completed?: boolean;
      prefix?: boolean;
      discussions?: boolean;
    }) => {
      const parameters = new URLSearchParams();
      for (const [name, value] of Object.entries(query)) {
        if (value !== undefined && value !== "") parameters.set(name, String(value));
      }
      return api.get<IssueSearch>(`/api/issues/search?${parameters}`);
    },
    searchDiscussion: (ref: string, q: string) =>
      api.get<DiscussionSearchResult[]>(
        `${issueAt(ref)}/discussion/search?${new URLSearchParams({ q })}`,
      ),

    labels: (key: string) => api.get<IssueLabel[]>(`${at(key)}/labels`),
    addLabel: (key: string, label: { name: string; colour?: string }) =>
      api.post<IssueLabel>(`${at(key)}/labels`, label),
    updateLabel: (key: string, id: string, changes: { name?: string; colour?: string }) =>
      api.patch<IssueLabel>(`${at(key)}/labels/${id}`, changes),
    removeLabel: (key: string, id: string) => api.delete<void>(`${at(key)}/labels/${id}`),
    addPriority: (key: string, name: string) =>
      api.post<IssuePriority>(`${at(key)}/priorities`, { name }),
    renamePriority: (key: string, id: string, name: string) =>
      api.patch<IssuePriority>(`${at(key)}/priorities/${id}`, { name }),
    reorderPriorities: (key: string, order: string[]) =>
      api.put<IssuePriority[]>(`${at(key)}/priorities`, { order }),
    removePriority: (key: string, id: string) => api.delete<void>(`${at(key)}/priorities/${id}`),

    assign: (ref: string, username: string) =>
      api.put<Issue>(`${issueAt(ref)}/assignees/${encodeURIComponent(username)}`),
    unassign: (ref: string, username: string) =>
      api.delete<Issue>(`${issueAt(ref)}/assignees/${encodeURIComponent(username)}`),
    label: (ref: string, labelId: string) => api.put<Issue>(`${issueAt(ref)}/labels/${labelId}`),
    unlabel: (ref: string, labelId: string) =>
      api.delete<Issue>(`${issueAt(ref)}/labels/${labelId}`),
    /** Sets an issue's priority; null clears it. */
    prioritise: (ref: string, priorityId: string | null) =>
      api.put<Issue>(`${issueAt(ref)}/priority`, { priorityId }),
    /** Relates this issue to another: kind is what this one is to that one. */
    relate: (ref: string, kind: RelationKind, other: string) =>
      api.post<Issue>(`${issueAt(ref)}/relations`, { kind, issue: other }),
    unrelate: (ref: string, kind: RelationKind, other: string) =>
      api.delete<Issue>(`${issueAt(ref)}/relations/${kind}/${encodeURIComponent(other)}`),
    file: (
      key: string,
      issue: {
        title: string;
        description?: string;
        stateId?: string;
        originChannel?: string;
        originEvent?: string;
      },
    ) => api.post<Issue>(`${at(key)}/issues`, issue),

    issue: (ref: string) => api.get<Issue>(issueAt(ref)),
    edit: (ref: string, changes: { title?: string; description?: string }) =>
      api.patch<Issue>(issueAt(ref), changes),
    remove: (ref: string) => api.delete<void>(issueAt(ref)),
    /**
     * Moves an issue. expectedStateId is the state it was seen in: given, a
     * move against an issue somebody else has just moved is refused with a
     * conflict saying what it became, rather than overwriting their move.
     */
    move: (ref: string, stateId: string, expectedStateId?: string) =>
      api.post<Issue>(`${issueAt(ref)}/state`, { stateId, expectedStateId }),
    history: (ref: string) => api.get<IssueHistoryEntry[]>(`${issueAt(ref)}/history`),

    /** Every state's first page of cards, narrowed by the filter if given. */
    board: (key: string, filter: BoardFilter = {}) => {
      const query = boardQuery(filter).toString();
      return api.get<Board>(`${at(key)}/board${query ? `?${query}` : ""}`);
    },
    /** More of one column, after the cursor its last page gave. */
    column: (key: string, stateId: string, after: string, filter: BoardFilter = {}) => {
      const query = boardQuery(filter);
      query.set("after", after);
      return api.get<BoardColumn>(`${at(key)}/board/${stateId}?${query}`);
    },

    /** Saved views: one project's, or those across projects when key is null. */
    views: (key: string | null) => api.get<SavedView[]>(key ? `${at(key)}/views` : "/api/views"),
    saveView: (
      key: string | null,
      view: { name: string; layout: "list" | "board"; filter: ViewFilter; shared: boolean },
    ) => api.post<SavedView>(key ? `${at(key)}/views` : "/api/views", view),
    updateView: (
      id: string,
      view: { name: string; layout: "list" | "board"; filter: ViewFilter; shared: boolean },
    ) => api.patch<SavedView>(`/api/views/${encodeURIComponent(id)}`, view),
    deleteView: (id: string) => api.delete<void>(`/api/views/${encodeURIComponent(id)}`),
    /** Opens a view as the requester: its results are what they may see. */
    openView: (id: string) => api.get<OpenedView>(`/api/views/${encodeURIComponent(id)}/results`),

    /** A project's calendar, and what happens to unfinished work when a cycle ends. */
    setPlanning: (key: string, planning: { timeZone?: string; cyclePolicy?: CyclePolicy }) =>
      api.put<Project>(`${at(key)}/planning`, planning),
    cycles: (key: string) => api.get<Cycle[]>(`${at(key)}/cycles`),
    cycle: (key: string, id: string) =>
      api.get<{ cycle: Cycle; issues: Issue[] }>(`${at(key)}/cycles/${id}`),
    /** Days are "YYYY-MM-DD", in the project's calendar. */
    createCycle: (key: string, cycle: { name: string; startsOn: string; endsOn: string }) =>
      api.post<Cycle>(`${at(key)}/cycles`, cycle),
    updateCycle: (
      key: string,
      id: string,
      cycle: { name: string; startsOn: string; endsOn: string },
    ) => api.patch<Cycle>(`${at(key)}/cycles/${id}`, cycle),
    deleteCycle: (key: string, id: string) => api.delete<void>(`${at(key)}/cycles/${id}`),
    milestones: (key: string) => api.get<Milestone[]>(`${at(key)}/milestones`),
    milestone: (key: string, id: string) =>
      api.get<{ milestone: Milestone; issues: Issue[]; cycles: Cycle[] }>(
        `${at(key)}/milestones/${id}`,
      ),
    createMilestone: (
      key: string,
      milestone: { name: string; description?: string; targetDate?: string },
    ) => api.post<Milestone>(`${at(key)}/milestones`, milestone),
    updateMilestone: (
      key: string,
      id: string,
      milestone: { name: string; description?: string; targetDate?: string },
    ) => api.patch<Milestone>(`${at(key)}/milestones/${id}`, milestone),
    deleteMilestone: (key: string, id: string) => api.delete<void>(`${at(key)}/milestones/${id}`),
    /**
     * How much is done, now and day by day: of the whole project, or of one
     * cycle or milestone. Counted when asked, so a move just made is in it.
     */
    progress: (key: string, of?: { cycle?: string; milestone?: string }) =>
      api.get<Progress>(
        of?.cycle
          ? `${at(key)}/cycles/${of.cycle}/progress`
          : of?.milestone
            ? `${at(key)}/milestones/${of.milestone}/progress`
            : `${at(key)}/progress`,
      ),
    /** Puts an issue into a cycle; null takes it out. */
    setCycle: (ref: string, cycleId: string | null) =>
      api.put<Issue>(`${issueAt(ref)}/cycle`, { cycleId }),
    /** Counts an issue towards a milestone; null towards none. */
    setMilestone: (ref: string, milestoneId: string | null) =>
      api.put<Issue>(`${issueAt(ref)}/milestone`, { milestoneId }),

    /**
     * Makes one change to many of a project's issues. Each issue is decided
     * on its own, and the answer says what became of every one.
     */
    bulk: (key: string, issues: string[], change: BulkChange) =>
      api.post<{ results: BulkResult[] }>(`${at(key)}/issues/bulk`, { issues, change }),
  };
}

function boardQuery(filter: BoardFilter): URLSearchParams {
  const query = new URLSearchParams();
  for (const name of ["label", "priority", "assignee", "cycle"] as const) {
    const value = filter[name];
    if (value) query.set(name, value);
  }
  return query;
}
