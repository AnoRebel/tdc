import type { Issue, IssueHistoryEntry, IssueState, Project, ProjectMember } from "@tdc/types";

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
    setMember: (key: string, username: string, role: "admin" | "member") =>
      api.put<void>(`${at(key)}/members/${encodeURIComponent(username)}`, { role }),
    removeMember: (key: string, username: string) =>
      api.delete<void>(`${at(key)}/members/${encodeURIComponent(username)}`),

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

    issues: (key: string, options: { state?: string; before?: number; limit?: number } = {}) => {
      const query = new URLSearchParams();
      if (options.state) query.set("state", options.state);
      if (options.before) query.set("before", String(options.before));
      if (options.limit) query.set("limit", String(options.limit));
      const suffix = query.size ? `?${query}` : "";
      return api.get<Issue[]>(`${at(key)}/issues${suffix}`);
    },
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
  };
}
