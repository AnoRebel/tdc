/** Somebody on this server, as an administrator sees them. */
export interface Person {
  id: string;
  username: string;
  displayName: string;
  address: string;
  createdAt: string;
  suspended: boolean;
  suspendedUntil?: string;
  suspensionNote?: string;
}

/** An invitation to this server. */
export interface ServerInvitation {
  id: string;
  url?: string;
  code?: string;
  createdBy: string;
  boundTo?: string;
  maxUses?: number;
  uses: number;
  expiresAt?: string;
  revokedAt?: string;
  usable: boolean;
  createdAt: string;
}

/**
 * Who is here, and who has been invited.
 *
 * Both need the permissions that govern them, so a refusal is expected rather
 * than exceptional and is reported as such — the page says you cannot see this
 * instead of implying something broke.
 */
export function usePeople() {
  const api = useApi();

  const people = ref<Person[]>([]);
  const invitations = ref<ServerInvitation[]>([]);
  const failed = ref(false);
  const loading = ref(false);

  async function refresh() {
    loading.value = true;
    try {
      /*
       * Settled rather than all: inviting and suspending are separate
       * permissions, so somebody may hold one and not the other. Failing both
       * because one was refused would hide a list they are entitled to.
       */
      const [loadedPeople, loadedInvitations] = await Promise.allSettled([
        api.get<Person[]>("/api/admin/users"),
        api.get<ServerInvitation[]>("/api/admin/invitations"),
      ]);

      people.value = loadedPeople.status === "fulfilled" ? loadedPeople.value : [];
      invitations.value = loadedInvitations.status === "fulfilled" ? loadedInvitations.value : [];

      failed.value = loadedPeople.status === "rejected" && loadedInvitations.status === "rejected";
    } finally {
      loading.value = false;
    }
  }

  /** Suspends somebody, optionally for a fixed number of hours. */
  async function suspend(userId: string, reason: string, hours?: number) {
    await api.post(`/api/admin/users/${userId}/suspend`, { reason, hours });
    await refresh();
  }

  async function lift(userId: string) {
    await api.delete(`/api/admin/users/${userId}/suspend`);
    await refresh();
  }

  /** Issues an invitation to the server. */
  async function invite(details: {
    boundTo?: string;
    maxUses?: number;
    expiresInHours?: number;
    withCode?: boolean;
  }): Promise<ServerInvitation> {
    const issued = await api.post<ServerInvitation>("/api/admin/invitations", details);
    await refresh();
    return issued;
  }

  async function revoke(invitationId: string) {
    await api.delete(`/api/invitations/${invitationId}`);
    await refresh();
  }

  return { people, invitations, failed, loading, refresh, suspend, lift, invite, revoke };
}
