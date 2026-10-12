<script setup lang="ts">
import { ArrowLeft, Check, Hash, Info, Minus, Search, ShieldCheck, X } from "lucide-vue-next";

import { isApiError } from "@tdc/types";
import { Avatar } from "#layers/ui/app/components/ui/avatar";
import { Button } from "#layers/ui/app/components/ui/button";
import { Select } from "#layers/ui/app/components/ui/select";
import { Skeleton } from "#layers/ui/app/components/ui/skeleton";
import { Spinner } from "#layers/ui/app/components/ui/spinner";
import type { EffectivePermission } from "#layers/ui/app/lib/roles";
import { deciding, grouped, isAdministrator } from "#layers/ui/app/lib/roles";
import type { PersonPermissions } from "#layers/ui/app/composables/useRoles";

const { $t, $ts, $localeRoute: localeRoute } = useI18n();

/**
 * What one person may do, and which of their roles says so.
 *
 * Every permission, decided by the server exactly as a request of theirs would
 * be — server-wide, or in one channel — with the entries that decided it and
 * the ones the rules set aside. This is how an administrator checks that a role
 * did what they meant before the person finds out the other way.
 *
 * Their roles can be changed from here too, which is where somebody looking at
 * a person rather than a role expects to do it.
 */

definePageMeta({ middleware: "auth", layout: "app" });

const route = useRoute();
const roles = useRoles();
const { can, user: me, refresh: refreshOwnPermissions } = useAuth();
const { describe, groupTitle } = usePermissionText();
const askFirst = useAskFirst();

const personId = computed(() => String(route.params.id));
const answer = ref<PersonPermissions | null>(null);
const failure = ref<unknown>(null);
const loading = ref(false);
const channel = ref<{ id: string; name: string } | null>(null);
const filter = ref("");
const error = ref<string | null>(null);

const refused = computed(() => isApiError(failure.value) && failure.value.status === 403);
const name = computed(() => answer.value?.user.displayName || answer.value?.user.username || "");

useHead({
  title: () => (name.value ? $ts("roles.personTitle", { name: name.value }) : $ts("roles.title")),
});

async function load() {
  loading.value = true;
  try {
    answer.value = await roles.explain(personId.value, channel.value?.id);
    failure.value = null;
  } catch (cause) {
    failure.value = cause;
  } finally {
    loading.value = false;
  }
}

onMounted(() => {
  void load();
  if (!roles.roles.value.length) void roles.refresh();
});
watch(() => [personId.value, channel.value?.id], load);

const administrator = computed(() => isAdministrator(answer.value?.permissions ?? []));

/** The catalogue's descriptions, by name, for the rows. */
const descriptions = computed(
  () =>
    new Map(roles.catalogue.value.map((definition) => [definition.name, definition.description])),
);

const sections = computed(() => {
  const needle = filter.value.trim().toLowerCase();
  const matching = (answer.value?.permissions ?? []).filter((permission) => {
    if (!needle) return true;
    const text = describe(permission.name, descriptions.value.get(permission.name) ?? "");
    return permission.name.toLowerCase().includes(needle) || text.toLowerCase().includes(needle);
  });
  return grouped(matching);
});

const allowedCount = computed(
  () => answer.value?.permissions.filter((permission) => permission.allowed).length ?? 0,
);

/** Why, in a sentence: which roles decided it, and where. */
function reason(permission: EffectivePermission): string {
  if (permission.basis === "administrator") return $ts("roles.reason.administrator");
  if (permission.basis === "none") return $ts("roles.reason.none");

  const { decided } = deciding(permission, administrator.value);
  const names = [...new Set(decided.map((source) => source.roleName))].join(", ");
  const here = permission.basis === "scope";
  if (permission.allowed) {
    return here
      ? $ts("roles.reason.grantedHere", { roles: names })
      : $ts("roles.reason.granted", { roles: names });
  }
  return here
    ? $ts("roles.reason.deniedHere", { roles: names })
    : $ts("roles.reason.denied", { roles: names });
}

function setAside(permission: EffectivePermission): string {
  const { overridden } = deciding(permission, administrator.value);
  return [...new Set(overridden.map((source) => source.roleName))].join(", ");
}

/* ---------- Their roles ---------- */

const choosing = ref<string>();
const busy = ref(false);

const offered = computed(() => {
  const holding = new Set(answer.value?.roles.map((role) => role.id));
  return roles.roles.value
    .filter((role) => !holding.has(role.id))
    .map((role) => ({ value: role.id, label: role.name }));
});

async function give() {
  if (!choosing.value) return;
  busy.value = true;
  error.value = null;
  try {
    await roles.assign(choosing.value, personId.value);
    choosing.value = undefined;
    await load();
    if (personId.value === me.value?.id) void refreshOwnPermissions();
  } catch (cause) {
    error.value = isApiError(cause) ? cause.message : $ts("roles.notAssigned");
  } finally {
    busy.value = false;
  }
}

async function takeAway(roleId: string) {
  if (!answer.value) return;
  error.value = null;
  const before = answer.value.roles;
  answer.value = { ...answer.value, roles: before.filter((role) => role.id !== roleId) };
  try {
    const done = await roles.unassign(roleId, personId.value, askFirst.ask);
    if (!done) {
      answer.value = { ...answer.value, roles: before };
      return;
    }
    await load();
    if (personId.value === me.value?.id) void refreshOwnPermissions();
  } catch (cause) {
    answer.value = { ...answer.value, roles: before };
    error.value = isApiError(cause) ? cause.message : $ts("roles.notRemoved");
  }
}
</script>

<template>
  <div class="flex-1 overflow-y-auto">
    <header class="flex h-12 shrink-0 items-center gap-3 border-b px-4">
      <NuxtLink
        :to="localeRoute({ name: 'roles' })"
        class="btn btn-ghost size-7 px-0"
        :aria-label="$ts('roles.backToRoles')"
      >
        <ArrowLeft class="size-4" />
      </NuxtLink>
      <h1 class="min-w-0 truncate text-sm font-semibold">
        {{ name ? $t("roles.personTitle", { name }) : $t("roles.title") }}
      </h1>
    </header>

    <div class="max-w-3xl space-y-8 px-4 py-6">
      <RolesRefusal v-if="refused" />

      <div v-else-if="loading && !answer" class="space-y-3" aria-busy="true">
        <Skeleton class="h-16 w-full" />
        <Skeleton class="h-72 w-full" />
      </div>

      <div
        v-else-if="failure || !answer"
        class="flex items-center justify-between gap-3 rounded-lg border border-destructive/30 bg-destructive/8 p-3 text-sm"
        role="alert"
      >
        <span class="text-destructive">
          {{
            isApiError(failure) && failure.status === 404
              ? $t("roles.personNotFound")
              : $t("roles.personLoadFailed")
          }}
        </span>
        <Button variant="outline" @click="load">{{ $t("action.retry") }}</Button>
      </div>

      <template v-else>
        <p
          v-if="error"
          class="sticky top-0 z-10 rounded-md bg-destructive/10 px-3 py-2 text-sm text-destructive backdrop-blur"
          role="alert"
        >
          {{ error }}
        </p>

        <section class="flex items-center gap-3">
          <Avatar :name="name" size="lg" />
          <div class="min-w-0">
            <p class="truncate text-sm font-semibold">{{ name }}</p>
            <p class="truncate text-xs text-muted-foreground">{{ answer.user.address }}</p>
          </div>
        </section>

        <!-- Their roles. -->
        <section>
          <h2 class="text-sm font-semibold">{{ $t("roles.theirRoles") }}</h2>
          <p v-if="!answer.roles.length" class="mt-2 text-xs text-muted-foreground">
            {{ $t("roles.noRolesHeld") }}
          </p>
          <ul v-else class="mt-2 flex flex-wrap gap-1.5">
            <li
              v-for="held in answer.roles"
              :key="held.id"
              class="flex items-center gap-1 rounded-full border bg-surface py-0.5 pr-1 pl-2.5 text-xs"
            >
              <ShieldCheck class="size-3 text-muted-foreground" aria-hidden="true" />
              <NuxtLink
                :to="localeRoute({ name: 'roles-id', params: { id: held.id } })"
                class="rounded hover:underline focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
              >
                {{ held.name }}
              </NuxtLink>
              <button
                v-if="can('role.assign')"
                type="button"
                class="grid size-5 place-items-center rounded-full text-muted-foreground transition-colors hover:bg-destructive/15 hover:text-destructive focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
                :aria-label="$ts('roles.removeFromRole', { name, role: held.name })"
                :title="$ts('roles.removeFromRole', { name, role: held.name })"
                @click="takeAway(held.id)"
              >
                <X class="size-3" />
              </button>
            </li>
          </ul>

          <div v-if="can('role.assign') && offered.length" class="mt-3 flex gap-2">
            <label for="give-role" class="sr-only">{{ $t("roles.giveRole") }}</label>
            <Select
              id="give-role"
              v-model="choosing"
              class="h-8 max-w-60"
              :options="offered"
              :label="$ts('roles.giveRole')"
              :placeholder="$ts('roles.chooseRole')"
            />
            <Button variant="outline" :disabled="!choosing || busy" @click="give">
              <Spinner v-if="busy" size="sm" />
              {{ $t("roles.giveRole") }}
            </Button>
          </div>
        </section>

        <!-- What that adds up to. -->
        <section>
          <div class="flex flex-wrap items-end justify-between gap-3">
            <div>
              <h2 class="text-sm font-semibold">{{ $t("roles.whatTheyMayDo") }}</h2>
              <p class="mt-0.5 text-xs text-muted-foreground" role="status">
                {{
                  channel
                    ? $t("roles.allowedCountHere", {
                        count: allowedCount,
                        total: answer.permissions.length,
                        channel: channel.name,
                      })
                    : $t("roles.allowedCount", {
                        count: allowedCount,
                        total: answer.permissions.length,
                      })
                }}
              </p>
            </div>
            <div class="relative w-full sm:w-56">
              <Search
                class="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-muted-foreground"
                aria-hidden="true"
              />
              <input
                v-model="filter"
                type="search"
                class="h-8 w-full rounded-md border border-border bg-surface pr-2 pl-8 text-sm placeholder:text-muted-foreground focus-visible:border-ring focus-visible:ring-2 focus-visible:ring-ring/40 focus-visible:outline-none"
                :aria-label="$ts('roles.filterPermissions')"
                :placeholder="$ts('roles.filterPermissions')"
              />
            </div>
          </div>

          <div class="mt-3 flex flex-wrap items-center gap-2">
            <span
              v-if="channel"
              class="flex items-center gap-1 rounded-full border bg-surface py-0.5 pr-1 pl-2 text-xs"
            >
              <Hash class="size-3 text-muted-foreground" aria-hidden="true" />
              {{ channel.name }}
              <button
                type="button"
                class="grid size-5 place-items-center rounded-full text-muted-foreground hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
                :aria-label="$ts('roles.everywhereInstead')"
                :title="$ts('roles.everywhereInstead')"
                @click="channel = null"
              >
                <X class="size-3" />
              </button>
            </span>
            <RolesSearchPicker
              v-else
              class="w-full sm:w-72"
              kind="rooms"
              :label="$ts('roles.checkInChannel')"
              :placeholder="$ts('roles.checkInChannel')"
              @pick="(room) => (channel = { id: room.id, name: room.name })"
            />
            <Spinner v-if="loading" size="sm" />
          </div>

          <p
            v-if="administrator"
            class="mt-3 flex gap-2 rounded-lg border border-warning/30 bg-warning/8 px-3 py-2 text-xs"
          >
            <Info class="mt-0.5 size-3.5 shrink-0 text-warning" aria-hidden="true" />
            {{ $t("roles.administratorNote") }}
          </p>

          <div v-for="section in sections" :key="section.group" class="mt-5">
            <h3 class="text-xs font-semibold tracking-wide text-muted-foreground uppercase">
              {{ groupTitle(section.group) }}
            </h3>
            <ul class="mt-1.5 divide-y rounded-lg border bg-surface">
              <li
                v-for="permission in section.permissions"
                :key="permission.name"
                class="flex items-start gap-2.5 px-3 py-2"
              >
                <span
                  class="mt-0.5 grid size-4 shrink-0 place-items-center rounded-full"
                  :class="
                    permission.allowed
                      ? 'bg-success/15 text-success-text'
                      : 'bg-muted text-muted-foreground'
                  "
                >
                  <component
                    :is="permission.allowed ? Check : Minus"
                    class="size-2.5"
                    aria-hidden="true"
                  />
                </span>
                <div class="min-w-0 flex-1">
                  <p class="text-sm" :class="permission.allowed ? '' : 'text-muted-foreground'">
                    <span class="sr-only">
                      {{ permission.allowed ? $t("roles.mayDo") : $t("roles.mayNotDo") }}
                    </span>
                    {{
                      describe(
                        permission.name,
                        descriptions.get(permission.name) ?? permission.name,
                      )
                    }}
                  </p>
                  <p
                    class="mt-0.5 flex flex-wrap items-center gap-x-2 text-[0.6875rem] text-muted-foreground"
                  >
                    <code class="font-mono">{{ permission.name }}</code>
                    <span
                      :class="
                        !permission.allowed && permission.basis !== 'none' ? 'text-destructive' : ''
                      "
                    >
                      {{ reason(permission) }}
                    </span>
                    <span
                      v-if="setAside(permission)"
                      class="line-through decoration-muted-foreground/60"
                    >
                      {{ $t("roles.reason.setAside", { roles: setAside(permission) }) }}
                    </span>
                  </p>
                </div>
              </li>
            </ul>
          </div>
        </section>
      </template>
    </div>

    <ConfirmDialog
      v-if="askFirst.pending.value"
      :title="$ts('roles.ownAuthorityTitle')"
      :detail="$ts('roles.ownAuthorityDetail', { permission: 'server.administer' })"
      :confirm-label="$ts('roles.ownAuthorityConfirm')"
      :cancel-label="$ts('action.cancel')"
      @cancel="askFirst.answer(false)"
      @confirm="askFirst.answer(true)"
    />
  </div>
</template>
