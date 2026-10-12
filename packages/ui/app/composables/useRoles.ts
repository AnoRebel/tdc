import type {
  EffectivePermission,
  EntryState,
  PermissionDefinition,
  Role,
} from "#layers/ui/app/lib/roles";
import { asksConfirmation, entryState, withEntry } from "#layers/ui/app/lib/roles";

/** Somebody holding a role, or being looked up. */
export interface RolePerson {
  id: string;
  username: string;
  displayName: string;
  address: string;
}

/** What somebody may do and why, as the server explains it. */
export interface PersonPermissions {
  user: RolePerson;
  roles: { id: string; name: string; builtIn: boolean }[];
  permissions: EffectivePermission[];
}

/**
 * Asked before a change that would take the asker's own authority.
 *
 * Supplied by the screen, which owns the dialog. Resolves true to go ahead.
 */
export type Confirm = () => Promise<boolean>;

/**
 * The question the server asks before somebody gives up their own authority.
 *
 * `ask` is handed to a change as its Confirm; the screen shows its dialog while
 * `pending` is set, and `answer` settles it.
 */
export function useAskFirst() {
  const pending = ref<((yes: boolean) => void) | null>(null);

  const ask: Confirm = () =>
    new Promise<boolean>((resolve) => {
      pending.value = resolve;
    });

  function answer(yes: boolean) {
    pending.value?.(yes);
    pending.value = null;
  }

  return { pending, ask, answer };
}

/**
 * A permission's description in the reader's language.
 *
 * The server's catalogue is the list and is in English; a translation is used
 * where one exists for the name, and the server's own words otherwise — so a
 * permission added to the server tomorrow is described, in English, rather
 * than missing.
 */
export function usePermissionText() {
  const { $ts, $has } = useI18n();

  function describe(name: string, fallback: string): string {
    const key = `roles.permission.${name.replace(/\./g, "_")}`;
    return $has(key) ? $ts(key) : fallback;
  }

  function groupTitle(group: string): string {
    return $ts(`roles.group.${group}`);
  }

  return { describe, groupTitle };
}

/**
 * Roles: what each allows, who holds it, and what somebody may do.
 *
 * Shared by the roles pages and the channel panel, through useState, so a
 * change made in one is what the other shows without a second fetch.
 *
 * Every change is applied here first and put back if the server refuses it.
 * A switch that waits a round trip feels broken; one that shows a state the
 * server refused is worse, so the rollback is explicit. Where the server asks
 * whether somebody means to give up their own authority, the question is put
 * to them and the change sent again only if they say yes.
 */
export function useRoles() {
  const api = useApi();

  const roles = useState<Role[]>("roles.list", () => []);
  const catalogue = useState<PermissionDefinition[]>("roles.catalogue", () => []);
  const loading = ref(false);
  /** The refusal, if reading was refused: a page shows it rather than an empty list. */
  const failure = ref<unknown>(null);

  async function refresh() {
    loading.value = true;
    try {
      const [listed, defined] = await Promise.all([
        api.get<Role[]>("/api/admin/roles"),
        catalogue.value.length
          ? Promise.resolve(catalogue.value)
          : api.get<PermissionDefinition[]>("/api/admin/permissions"),
      ]);
      roles.value = listed;
      catalogue.value = defined;
      failure.value = null;
    } catch (cause) {
      failure.value = cause;
    } finally {
      loading.value = false;
    }
  }

  function replace(role: Role) {
    roles.value = roles.value.map((existing) => (existing.id === role.id ? role : existing));
  }

  /**
   * Sets what a role says about a permission, server-wide or in one channel.
   *
   * Resolves false when the person declined to go ahead; throws the server's
   * refusal otherwise, after putting the role back as it was.
   */
  async function setEntry(
    roleId: string,
    permission: string,
    state: EntryState,
    confirm: Confirm,
    scope?: { id: string; name?: string },
  ): Promise<boolean> {
    const before = roles.value.find((role) => role.id === roleId);
    if (!before || entryState(before, permission, scope?.id) === state) return true;

    replace(withEntry(before, permission, state, scope));

    const send = (confirmed: boolean) => {
      if (state === "inherit") {
        const query = new URLSearchParams();
        if (scope) query.set("channel", scope.id);
        if (confirmed) query.set("confirmed", "true");
        const suffix = query.size ? `?${query}` : "";
        return api.delete(
          `/api/admin/roles/${roleId}/permissions/${encodeURIComponent(permission)}${suffix}`,
        );
      }
      return api.put(`/api/admin/roles/${roleId}/permissions`, {
        permission,
        granted: state === "grant",
        ...(scope ? { channel: scope.id } : {}),
        ...(confirmed ? { confirmed: true } : {}),
      });
    };

    try {
      try {
        await send(false);
      } catch (cause) {
        if (!asksConfirmation(cause)) throw cause;
        if (!(await confirm())) {
          replace(before);
          return false;
        }
        await send(true);
      }
      return true;
    } catch (cause) {
      replace(before);
      throw cause;
    }
  }

  async function create(name: string, description: string): Promise<Role> {
    const created = await api.post<Omit<Role, "memberCount" | "permissions">>("/api/admin/roles", {
      name,
      description,
    });
    const role: Role = { ...created, memberCount: 0, permissions: [] };
    roles.value = [...roles.value, role];
    return role;
  }

  async function update(roleId: string, name: string, description: string) {
    const before = roles.value.find((role) => role.id === roleId);
    if (!before) return;
    replace({ ...before, name, description });
    try {
      const saved = await api.patch<Pick<Role, "name" | "description">>(
        `/api/admin/roles/${roleId}`,
        {
          name,
          description,
        },
      );
      const now = roles.value.find((role) => role.id === roleId);
      if (now) replace({ ...now, name: saved.name, description: saved.description });
    } catch (cause) {
      replace(before);
      throw cause;
    }
  }

  async function remove(roleId: string) {
    await api.delete(`/api/admin/roles/${roleId}`);
    roles.value = roles.value.filter((role) => role.id !== roleId);
  }

  async function members(roleId: string) {
    return api.get<{ members: RolePerson[]; truncated: boolean }>(
      `/api/admin/roles/${roleId}/members`,
    );
  }

  function adjustCount(roleId: string, by: number) {
    const role = roles.value.find((candidate) => candidate.id === roleId);
    if (role) replace({ ...role, memberCount: Math.max(0, role.memberCount + by) });
  }

  async function assign(roleId: string, userId: string) {
    await api.post(`/api/admin/roles/${roleId}/members`, { user: userId });
    adjustCount(roleId, 1);
  }

  /** Takes somebody out of a role; resolves false if they declined to confirm. */
  async function unassign(roleId: string, userId: string, confirm: Confirm): Promise<boolean> {
    const path = `/api/admin/roles/${roleId}/members/${userId}`;
    try {
      await api.delete(path);
    } catch (cause) {
      if (!asksConfirmation(cause)) throw cause;
      if (!(await confirm())) return false;
      await api.delete(`${path}?confirmed=true`);
    }
    adjustCount(roleId, -1);
    return true;
  }

  function explain(userId: string, channelId?: string) {
    const suffix = channelId ? `?channel=${encodeURIComponent(channelId)}` : "";
    return api.get<PersonPermissions>(`/api/admin/users/${userId}/permissions${suffix}`);
  }

  /** People and channels matching what somebody typed, for the pickers. */
  async function search(text: string) {
    if (!text.trim())
      return { people: [] as RolePerson[], rooms: [] as { id: string; name: string }[] };
    const query = new URLSearchParams({ q: text, messages: "false", limit: "8" });
    return api.get<{ people: RolePerson[]; rooms: { id: string; name: string }[] }>(
      `/api/search/everything?${query}`,
    );
  }

  return {
    roles,
    catalogue,
    loading,
    failure,
    refresh,
    setEntry,
    create,
    update,
    remove,
    members,
    assign,
    unassign,
    explain,
    search,
  };
}
