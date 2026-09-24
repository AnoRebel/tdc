import type {
  Company,
  CompanyAssociation,
  DirectoryFilters,
  DirectoryResults,
  DirectoryVisibility,
  OwnProfile,
  ProfileDocument,
  ProfileView,
  QueuedSkill,
  Skill,
} from "@tdc/types";
import { isApiError } from "@tdc/types";

/**
 * A profile nobody has written yet, in the shape the server saves.
 *
 * The visibilities match the server's own defaults, and contact details start
 * hidden: a form somebody submits without looking publishes nothing they did
 * not choose to.
 */
export function emptyProfile(): ProfileDocument {
  return {
    headline: "",
    bio: "",
    location: "",
    experience: "",
    contactEmail: "",
    contactPhone: "",
    contactUrl: "",
    visibility: "members",
    detailsVisibility: "members",
    contactVisibility: "hidden",
    portfolio: [],
    skills: [],
    openToWork: false,
    availabilityNote: "",
  };
}

/**
 * The directory, as the signed-in person uses it.
 *
 * Thin on purpose: every rule about who sees what is the server's, decided in
 * its queries, so nothing here filters or hides — it asks and shows what it is
 * given. A client that tried to enforce visibility would be a second place for
 * the rule to be wrong.
 */
export function useDirectory() {
  const api = useApi();

  /** Somebody's own profile, or null when they have not made one. */
  async function ownProfile(): Promise<OwnProfile | null> {
    try {
      return await api.get<OwnProfile>("/api/directory/me");
    } catch (cause) {
      if (isApiError(cause) && cause.code === "not-found") return null;
      throw cause;
    }
  }

  function saveProfile(profile: ProfileDocument) {
    return api.put<OwnProfile>("/api/directory/me", profile);
  }

  function deleteProfile() {
    return api.delete<void>("/api/directory/me");
  }

  function exportProfile() {
    return api.get<Record<string, unknown>>("/api/directory/me/export");
  }

  /** Somebody's profile as the signed-in person may see it. */
  function viewProfile(username: string) {
    return api.get<ProfileView>(`/api/directory/profiles/${encodeURIComponent(username)}`);
  }

  function myCompanies() {
    return api.get<CompanyAssociation[]>("/api/directory/me/companies");
  }

  function acceptCompany(slug: string) {
    return api.post<void>(`/api/directory/me/companies/${encodeURIComponent(slug)}/accept`);
  }

  function leaveCompany(slug: string) {
    return api.delete<void>(`/api/directory/me/companies/${encodeURIComponent(slug)}`);
  }

  function viewCompany(slug: string) {
    return api.get<Company>(`/api/directory/companies/${encodeURIComponent(slug)}`);
  }

  function createCompany(company: {
    slug: string;
    name: string;
    description: string;
    location: string;
    visibility: Exclude<DirectoryVisibility, "hidden">;
    role: string;
  }) {
    return api.post<Company>("/api/directory/companies", company);
  }

  function updateCompany(
    slug: string,
    company: {
      name: string;
      description: string;
      location: string;
      visibility: Exclude<DirectoryVisibility, "hidden">;
    },
  ) {
    return api.put<Company>(`/api/directory/companies/${encodeURIComponent(slug)}`, company);
  }

  function deleteCompany(slug: string) {
    return api.delete<void>(`/api/directory/companies/${encodeURIComponent(slug)}`);
  }

  function inviteToCompany(slug: string, username: string, role: string) {
    return api.post<void>(`/api/directory/companies/${encodeURIComponent(slug)}/members`, {
      username,
      role,
    });
  }

  function removeFromCompany(slug: string, username: string) {
    return api.delete<void>(
      `/api/directory/companies/${encodeURIComponent(slug)}/members/${encodeURIComponent(username)}`,
    );
  }

  /**
   * One page of a directory search.
   *
   * There is no page size to ask for: the server decides it, and a client
   * wanting more asks for the next page.
   */
  function search(filters: DirectoryFilters) {
    const query = new URLSearchParams();
    if (filters.q?.trim()) query.set("q", filters.q.trim());
    if (filters.skill?.trim()) query.set("skill", filters.skill.trim());
    if (filters.available) query.set("available", "true");
    if (filters.location?.trim()) query.set("location", filters.location.trim());
    if (filters.company?.trim()) query.set("company", filters.company.trim());
    if (filters.page) query.set("page", String(filters.page));
    return api.get<DirectoryResults>(`/api/directory/search?${query.toString()}`);
  }

  /** The vocabulary matching what somebody typed, with their own proposals. */
  function skills(query = "") {
    return api.get<Skill[]>(`/api/directory/skills?q=${encodeURIComponent(query)}`);
  }

  /**
   * Proposes a skill. The server answers with the skill the name already
   * means, where it means one, rather than making a second.
   */
  function proposeSkill(name: string, description = "") {
    return api.post<Skill>("/api/directory/skills", { name, description });
  }

  function skillQueue() {
    return api.get<QueuedSkill[]>("/api/directory/moderation/skills");
  }

  function approveSkill(id: string) {
    return api.post<Skill>(`/api/directory/moderation/skills/${id}/approve`);
  }

  function rejectSkill(id: string, reason: string) {
    return api.post<void>(`/api/directory/moderation/skills/${id}/reject`, { reason });
  }

  function mergeSkill(id: string, into: string) {
    return api.post<Skill>(`/api/directory/moderation/skills/${id}/merge`, { into });
  }

  return {
    search,
    skills,
    proposeSkill,
    skillQueue,
    approveSkill,
    rejectSkill,
    mergeSkill,
    ownProfile,
    saveProfile,
    deleteProfile,
    exportProfile,
    viewProfile,
    myCompanies,
    acceptCompany,
    leaveCompany,
    viewCompany,
    createCompany,
    updateCompany,
    deleteCompany,
    inviteToCompany,
    removeFromCompany,
  };
}
