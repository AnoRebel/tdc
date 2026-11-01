<script setup lang="ts">
import { nameOf } from "#layers/ui/app/lib/names";
import { useForm } from "@tanstack/vue-form";
import type { AnyFieldApi } from "@tanstack/vue-form";
import * as v from "valibot";
import { ArrowLeft, Hash, Info, Search, Trash2, UserMinus, Users, X } from "lucide-vue-next";

import { isApiError } from "@tdc/types";
import { Avatar } from "#layers/ui/app/components/ui/avatar";
import { Button } from "#layers/ui/app/components/ui/button";
import { Field } from "#layers/ui/app/components/ui/field";
import { Input } from "#layers/ui/app/components/ui/input";
import { Skeleton } from "#layers/ui/app/components/ui/skeleton";
import { Spinner } from "#layers/ui/app/components/ui/spinner";
import { Textarea } from "#layers/ui/app/components/ui/textarea";
import type { EntryState, RoleEntry } from "#layers/ui/app/lib/roles";
import {
  ADMINISTER,
  BASELINE_ROLE,
  administers,
  consequence,
  entryState,
  grouped,
  isBaseline,
} from "#layers/ui/app/lib/roles";
import type { RolePerson } from "#layers/ui/app/composables/useRoles";

const { $t, $ts, $localeRoute: localeRoute } = useI18n();

/**
 * One role: what it is called, what it allows and withholds, and who holds it.
 *
 * Each permission is one of three things — allowed, denied, or not mentioned —
 * and beside each is what that will mean, worked out from the rules the server
 * applies: Member is a baseline any other role's entry replaces, a denial beats
 * every grant, and administration implies everything not denied. Somebody
 * setting up a moderator should not have to know those rules to get it right.
 *
 * Every change is shown at once and put back if the server refuses it.
 */

definePageMeta({ middleware: "auth", layout: "app" });

const route = useRoute();
const router = useRouter();
const roles = useRoles();
const { can, user, refresh: refreshOwnPermissions } = useAuth();
const { describe, groupTitle } = usePermissionText();
const askFirst = useAskFirst();

const roleId = computed(() => String(route.params.id));
const role = computed(() => roles.roles.value.find((candidate) => candidate.id === roleId.value));
const member = computed(() =>
  roles.roles.value.find((candidate) => candidate.builtIn && candidate.name === BASELINE_ROLE),
);

useHead({ title: () => role.value?.name ?? $ts("roles.title") });

const mayEdit = computed(() => can("role.manage"));
const mayAssign = computed(() => can("role.assign"));
const refused = computed(
  () => isApiError(roles.failure.value) && roles.failure.value.status === 403,
);

const error = ref<string | null>(null);
function report(cause: unknown, fallback: string) {
  error.value = isApiError(cause) ? cause.message : fallback;
}

onMounted(async () => {
  if (!roles.roles.value.length) await roles.refresh();
  resetDetails();
  await loadMembers();
});

/* ---------- Name and description ---------- */

const details = v.object({
  name: v.pipe(
    v.string(),
    v.trim(),
    v.minLength(1, $ts("roles.nameRequired")),
    v.maxLength(64, $ts("roles.nameTooLong")),
  ),
  description: v.pipe(v.string(), v.trim(), v.maxLength(500, $ts("roles.descriptionTooLong"))),
});

function firstError(field: AnyFieldApi): string | undefined {
  if (!field.state.meta.isTouched) return undefined;
  const [issue] = field.state.meta.errors;
  if (!issue) return undefined;
  return typeof issue === "string" ? issue : (issue as { message?: string }).message;
}

const saved = ref(false);
const form = useForm({
  defaultValues: { name: "", description: "" },
  validators: { onSubmit: details },
  async onSubmit({ value }) {
    error.value = null;
    try {
      await roles.update(roleId.value, value.name.trim(), value.description.trim());
      saved.value = true;
      setTimeout(() => (saved.value = false), 2000);
    } catch (cause) {
      report(cause, $ts("roles.notSaved"));
      resetDetails();
    }
  },
});

function resetDetails() {
  form.reset({ name: role.value?.name ?? "", description: role.value?.description ?? "" });
}
watch(() => role.value?.id, resetDetails);

/* ---------- Deleting ---------- */

const deleting = ref(false);

/** What deleting takes from the people holding the role, said plainly. */
const deletionDetail = computed(() => {
  if (!role.value) return "";
  const allowed = role.value.permissions
    .filter((entry) => entry.granted && entry.scope === "server")
    .map((entry) => entry.permission);
  const shown = allowed.slice(0, 3).join(", ");
  const lost = allowed.length
    ? allowed.length > 3
      ? $ts("roles.deleteLosesMore", { list: shown, count: allowed.length - 3 })
      : $ts("roles.deleteLoses", { list: shown })
    : $ts("roles.deleteLosesNothing");
  return `${$ts("roles.deleteHolders", { count: role.value.memberCount })} ${lost}`;
});

async function confirmDelete() {
  deleting.value = false;
  error.value = null;
  try {
    await roles.remove(roleId.value);
    await router.push(localeRoute({ name: "roles" }));
  } catch (cause) {
    report(cause, $ts("roles.notDeleted"));
  }
}

/* ---------- Permissions ---------- */

const filter = ref("");
const sections = computed(() => {
  const needle = filter.value.trim().toLowerCase();
  const matching = roles.catalogue.value.filter(
    (definition) =>
      !needle ||
      definition.name.toLowerCase().includes(needle) ||
      describe(definition.name, definition.description).toLowerCase().includes(needle),
  );
  return grouped(matching);
});

const pending = ref(new Set<string>());

async function setState(
  permission: string,
  state: EntryState,
  scope?: { id: string; name?: string },
) {
  const key = `${permission}:${scope?.id ?? ""}`;
  error.value = null;
  pending.value = new Set(pending.value).add(key);
  try {
    await roles.setEntry(roleId.value, permission, state, askFirst.ask, scope);
    // Changing a role one holds can change what one may do here.
    if (held.value) void refreshOwnPermissions();
  } catch (cause) {
    report(cause, $ts("roles.notChanged"));
  } finally {
    const next = new Set(pending.value);
    next.delete(key);
    pending.value = next;
  }
}

const scoped = computed<RoleEntry[]>(
  () => role.value?.permissions.filter((entry) => entry.scope !== "server") ?? [],
);

/* ---------- Who holds it ---------- */

const people = ref<RolePerson[]>([]);
const truncated = ref(false);
const loadingMembers = ref(false);
const held = computed(() => people.value.some((person) => person.id === user.value?.id));

async function loadMembers() {
  loadingMembers.value = true;
  try {
    const listed = await roles.members(roleId.value);
    people.value = listed.members;
    truncated.value = listed.truncated;
  } catch {
    people.value = [];
  } finally {
    loadingMembers.value = false;
  }
}

async function add(choice: { id: string; name: string; detail?: string }) {
  error.value = null;
  const person: RolePerson = {
    id: choice.id,
    username: choice.name,
    displayName: choice.name,
    address: choice.detail ?? "",
  };
  people.value = [...people.value, person];
  try {
    await roles.assign(roleId.value, choice.id);
    if (choice.id === user.value?.id) void refreshOwnPermissions();
  } catch (cause) {
    people.value = people.value.filter((candidate) => candidate.id !== choice.id);
    report(cause, $ts("roles.notAssigned"));
  }
}

async function takeOut(person: RolePerson) {
  error.value = null;
  const before = people.value;
  people.value = people.value.filter((candidate) => candidate.id !== person.id);
  try {
    const done = await roles.unassign(roleId.value, person.id, askFirst.ask);
    if (!done) people.value = before;
    else if (person.id === user.value?.id) void refreshOwnPermissions();
  } catch (cause) {
    people.value = before;
    report(cause, $ts("roles.notRemoved"));
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
      <h1 class="min-w-0 truncate text-sm font-semibold">{{ role?.name ?? $t("roles.title") }}</h1>
      <span
        v-if="role?.builtIn"
        class="rounded bg-muted px-1.5 py-0.5 text-[0.6875rem] text-muted-foreground"
      >
        {{ $t("roles.builtIn") }}
      </span>
    </header>

    <div class="max-w-3xl space-y-8 px-4 py-6">
      <RolesRefusal v-if="refused" />

      <div v-else-if="roles.loading.value && !role" class="space-y-3" aria-busy="true">
        <Skeleton class="h-24 w-full" />
        <Skeleton class="h-64 w-full" />
      </div>

      <div
        v-else-if="!role"
        class="flex items-center justify-between gap-3 rounded-lg border p-4 text-sm"
        role="alert"
      >
        <span>{{ roles.failure.value ? $t("roles.loadFailed") : $t("roles.notFound") }}</span>
        <Button v-if="roles.failure.value" variant="outline" @click="roles.refresh()">
          {{ $t("action.retry") }}
        </Button>
        <NuxtLink v-else :to="localeRoute({ name: 'roles' })" class="btn btn-outline">
          {{ $t("roles.backToRoles") }}
        </NuxtLink>
      </div>

      <template v-else>
        <!-- One place for every refusal, announced as it appears. Sticky, so a
             toggle refused far down the list is not reported off screen. -->
        <p
          v-if="error"
          class="sticky top-0 z-10 flex items-start gap-2 rounded-md bg-destructive/10 px-3 py-2 text-sm text-destructive backdrop-blur"
          role="alert"
        >
          <span class="min-w-0 flex-1">{{ error }}</span>
          <button
            type="button"
            class="shrink-0 rounded p-0.5 hover:bg-destructive/15"
            :aria-label="$ts('action.close')"
            @click="error = null"
          >
            <X class="size-3.5" />
          </button>
        </p>

        <!-- What it is. -->
        <section>
          <form
            v-if="mayEdit"
            class="space-y-3"
            :aria-label="$ts('roles.detailsLabel')"
            @submit.prevent="form.handleSubmit()"
          >
            <form.Field name="name">
              <template #default="{ field }">
                <Field
                  :label="$ts('roles.name')"
                  :hint="role.builtIn ? $ts('roles.builtInNameHint') : undefined"
                  :error="firstError(field)"
                  required
                >
                  <template #default="{ id, invalid, describedBy }">
                    <Input
                      :id="id"
                      :model-value="field.state.value"
                      :disabled="role.builtIn"
                      :invalid="invalid"
                      :described-by="describedBy"
                      @update:model-value="field.handleChange($event)"
                      @blur="field.handleBlur"
                    />
                  </template>
                </Field>
              </template>
            </form.Field>
            <form.Field name="description">
              <template #default="{ field }">
                <Field :label="$ts('roles.description')" :error="firstError(field)">
                  <template #default="{ id, describedBy }">
                    <Textarea
                      :id="id"
                      class="h-auto py-2"
                      rows="2"
                      :model-value="field.state.value"
                      :aria-describedby="describedBy"
                      @update:model-value="field.handleChange(String($event))"
                      @blur="field.handleBlur"
                    />
                  </template>
                </Field>
              </template>
            </form.Field>
            <div class="flex items-center justify-between gap-2">
              <Button v-if="!role.builtIn" variant="danger" @click="deleting = true">
                <Trash2 class="size-3.5" aria-hidden="true" />
                {{ $t("roles.delete") }}
              </Button>
              <span v-else class="text-xs text-muted-foreground">{{
                $t("roles.builtInHint")
              }}</span>
              <span class="flex items-center gap-2">
                <span v-if="saved" class="text-xs text-success-text" role="status">
                  {{ $t("roles.saved") }}
                </span>
                <Button type="submit" :disabled="form.state.isSubmitting">
                  <Spinner v-if="form.state.isSubmitting" size="sm" />
                  {{ $t("action.save") }}
                </Button>
              </span>
            </div>
          </form>
          <p v-else class="text-sm text-muted-foreground">
            {{ role.description || $t("roles.noDescription") }}
          </p>
        </section>

        <!-- What it allows. -->
        <section>
          <div class="flex flex-wrap items-end justify-between gap-3">
            <div>
              <h2 class="text-sm font-semibold">{{ $t("roles.permissions") }}</h2>
              <p class="mt-0.5 text-xs text-muted-foreground">{{ $t("roles.permissionsHint") }}</p>
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

          <p
            v-if="isBaseline(role)"
            class="mt-3 flex gap-2 rounded-lg border border-primary/30 bg-primary/8 px-3 py-2 text-xs"
          >
            <Info class="mt-0.5 size-3.5 shrink-0 text-primary" aria-hidden="true" />
            {{ $t("roles.baselineNote") }}
          </p>
          <p
            v-else-if="administers(role)"
            class="mt-3 flex gap-2 rounded-lg border border-warning/30 bg-warning/8 px-3 py-2 text-xs"
          >
            <Info class="mt-0.5 size-3.5 shrink-0 text-warning" aria-hidden="true" />
            {{ $t("roles.administersNote") }}
          </p>

          <p v-if="!sections.length" class="mt-3 text-xs text-muted-foreground">
            {{ $t("roles.noPermissionMatches") }}
          </p>

          <div v-for="section in sections" :key="section.group" class="mt-5">
            <h3 class="text-xs font-semibold tracking-wide text-muted-foreground uppercase">
              {{ groupTitle(section.group) }}
            </h3>
            <ul class="mt-1.5 divide-y rounded-lg border bg-surface">
              <li
                v-for="definition in section.permissions"
                :key="definition.name"
                class="flex flex-col gap-2 px-3 py-2 sm:flex-row sm:items-center sm:gap-4"
              >
                <div class="min-w-0 flex-1">
                  <p class="flex flex-wrap items-center gap-1.5">
                    <span class="text-sm">{{
                      describe(definition.name, definition.description)
                    }}</span>
                    <span
                      v-if="definition.administrative"
                      class="rounded bg-warning/15 px-1 py-px text-[0.625rem] font-medium text-warning"
                    >
                      {{ $t("roles.administrative") }}
                    </span>
                  </p>
                  <p class="mt-0.5 flex flex-wrap items-center gap-x-2 text-[0.6875rem]">
                    <code class="font-mono text-muted-foreground">{{ definition.name }}</code>
                    <span
                      :class="
                        entryState(role, definition.name) === 'deny'
                          ? 'text-destructive'
                          : 'text-muted-foreground'
                      "
                    >
                      {{ $t(`roles.consequence.${consequence(role, definition.name, member)}`) }}
                    </span>
                  </p>
                </div>
                <span class="flex items-center gap-2 self-end sm:self-auto">
                  <Spinner v-if="pending.has(`${definition.name}:`)" size="sm" />
                  <RolesEntryToggle
                    :model-value="entryState(role, definition.name)"
                    :label="definition.name"
                    :disabled="!mayEdit"
                    @update:model-value="(state: EntryState) => setState(definition.name, state)"
                  />
                </span>
              </li>
            </ul>
          </div>
        </section>

        <!-- What it says in particular channels. -->
        <section>
          <h2 class="text-sm font-semibold">{{ $t("roles.inChannels") }}</h2>
          <p class="mt-0.5 text-xs text-muted-foreground">{{ $t("roles.inChannelsHint") }}</p>
          <p v-if="!scoped.length" class="mt-3 text-xs text-muted-foreground">
            {{ $t("roles.noChannelEntries") }}
          </p>
          <ul v-else class="mt-3 divide-y rounded-lg border bg-surface">
            <li
              v-for="entry in scoped"
              :key="`${entry.permission}:${entry.scopeId}`"
              class="flex items-center gap-3 px-3 py-2"
            >
              <Hash class="size-3.5 shrink-0 text-muted-foreground" aria-hidden="true" />
              <span class="min-w-0 flex-1">
                <span class="block truncate text-sm">
                  {{
                    entry.scopeName ||
                    (entry.scope === "project" ? $t("roles.goneProject") : $t("roles.goneChannel"))
                  }}
                  <span v-if="entry.scope === 'project'" class="text-xs text-muted-foreground">
                    · {{ $t("roles.project") }}
                  </span>
                </span>
                <code class="font-mono text-[0.6875rem] text-muted-foreground">{{
                  entry.permission
                }}</code>
              </span>
              <span
                class="rounded px-1.5 py-0.5 text-[0.6875rem] font-medium"
                :class="
                  entry.granted
                    ? 'bg-success/15 text-success-text'
                    : 'bg-destructive/15 text-destructive'
                "
              >
                {{ entry.granted ? $t("roles.state.grant") : $t("roles.state.deny") }}
              </span>
              <Button
                v-if="mayEdit"
                variant="ghost"
                size="small"
                :label="$ts('roles.clearEntry', { permission: entry.permission })"
                :disabled="pending.has(`${entry.permission}:${entry.scopeId}`)"
                @click="setState(entry.permission, 'inherit', { id: entry.scopeId ?? '' })"
              >
                {{ $t("roles.clear") }}
              </Button>
            </li>
          </ul>
        </section>

        <!-- Who holds it. -->
        <section>
          <h2 class="flex items-center gap-1.5 text-sm font-semibold">
            <Users class="size-4" aria-hidden="true" />
            {{ $t("roles.holdersTitle") }}
            <span class="tabular text-xs font-normal text-muted-foreground">{{
              role.memberCount
            }}</span>
          </h2>
          <p v-if="isBaseline(role)" class="mt-0.5 text-xs text-muted-foreground">
            {{ $t("roles.everyoneHoldsMember") }}
          </p>

          <RolesSearchPicker
            v-if="mayAssign"
            class="mt-3"
            kind="people"
            :label="$ts('roles.addPerson')"
            :placeholder="$ts('roles.addPerson')"
            :exclude="people.map((person) => person.id)"
            @pick="add"
          />

          <p
            v-if="loadingMembers"
            class="mt-3 flex items-center gap-2 text-xs text-muted-foreground"
          >
            <Spinner size="sm" />
            {{ $t("roles.loadingHolders") }}
          </p>
          <p v-else-if="!people.length" class="mt-3 text-xs text-muted-foreground">
            {{ $t("roles.nobodyHolds") }}
          </p>
          <TransitionGroup
            v-else
            tag="ul"
            class="mt-3 space-y-1"
            enter-active-class="transition-[opacity,transform] duration-(--motion-duration) ease-(--motion-ease-out)"
            enter-from-class="opacity-0 -translate-y-1"
            leave-active-class="transition-opacity duration-(--motion-duration-fast)"
            leave-to-class="opacity-0"
          >
            <li
              v-for="person in people"
              :key="person.id"
              class="flex items-center gap-2.5 rounded-lg px-2 py-1.5 transition-colors hover:bg-accent"
            >
              <Avatar :address="person.address" :name="nameOf(person)" size="sm" />
              <NuxtLink
                :to="localeRoute({ name: 'roles-people-id', params: { id: person.id } })"
                class="min-w-0 flex-1 rounded focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
              >
                <span class="block truncate text-sm font-medium">
                  {{ nameOf(person) }}
                </span>
                <span class="block truncate text-xs text-muted-foreground">{{
                  person.address
                }}</span>
              </NuxtLink>
              <Button
                v-if="mayAssign"
                variant="danger"
                size="icon"
                :label="
                  $ts('roles.removeFromRole', {
                    name: nameOf(person),
                    role: role.name,
                  })
                "
                @click="takeOut(person)"
              >
                <UserMinus class="size-3.5" />
              </Button>
            </li>
          </TransitionGroup>
          <p v-if="truncated" class="mt-2 text-xs text-muted-foreground">
            {{ $t("roles.holdersTruncated", { count: people.length }) }}
          </p>
        </section>
      </template>
    </div>

    <ConfirmDialog
      v-if="deleting && role"
      :title="$ts('roles.deleteTitle', { name: role.name })"
      :detail="deletionDetail"
      :confirm-label="$ts('roles.delete')"
      :cancel-label="$ts('action.cancel')"
      @cancel="deleting = false"
      @confirm="confirmDelete"
    />

    <ConfirmDialog
      v-if="askFirst.pending.value"
      :title="$ts('roles.ownAuthorityTitle')"
      :detail="$ts('roles.ownAuthorityDetail', { permission: ADMINISTER })"
      :confirm-label="$ts('roles.ownAuthorityConfirm')"
      :cancel-label="$ts('action.cancel')"
      @cancel="askFirst.answer(false)"
      @confirm="askFirst.answer(true)"
    />
  </div>
</template>
