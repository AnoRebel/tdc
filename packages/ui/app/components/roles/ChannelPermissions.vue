<script setup lang="ts">
import { isApiError } from "@tdc/types";
import { Select } from "#layers/ui/app/components/ui/select";
import { Spinner } from "#layers/ui/app/components/ui/spinner";
import type { EntryState } from "#layers/ui/app/lib/roles";
import { BASELINE_ROLE, entryState, grouped } from "#layers/ui/app/lib/roles";

/**
 * Who may do what in one channel.
 *
 * A role's entries for this channel alone, which beat what the role says
 * server-wide here and nowhere else. The usual uses are two: withdrawing
 * posting from Member and granting it to an Announcer role, which makes an
 * announcement channel; and granting a role the four permissions that look
 * after a channel, which makes its keepers without giving them a say anywhere
 * else.
 *
 * Loaded only when somebody who may change roles opens a channel's settings,
 * so nobody else pays for it.
 */

const { $t, $ts } = useI18n();

const props = defineProps<{ roomId: string; roomName: string }>();

const roles = useRoles();
const { describe, groupTitle } = usePermissionText();
const askFirst = useAskFirst();

const chosen = ref<string>();
const error = ref<string | null>(null);
const pending = ref<string | null>(null);

onMounted(async () => {
  if (!roles.roles.value.length) await roles.refresh();
  chosen.value ??= roles.roles.value.find(
    (role) => role.builtIn && role.name === BASELINE_ROLE,
  )?.id;
});

const role = computed(() => roles.roles.value.find((candidate) => candidate.id === chosen.value));

/** Only what can be granted for one channel. */
const sections = computed(() =>
  grouped(roles.catalogue.value.filter((definition) => definition.scopes.includes("channel"))),
);

/** Every role's entries here, so the whole picture is one glance. */
const here = computed(() =>
  roles.roles.value.flatMap((candidate) =>
    candidate.permissions
      .filter((entry) => entry.scopeId === props.roomId)
      .map((entry) => ({ role: candidate.name, ...entry })),
  ),
);

/** What applies here when the role says nothing for this channel. */
function serverWide(permission: string): string {
  const state = entryState(role.value, permission);
  return $ts(`roles.channel.serverWide.${state}`);
}

async function set(permission: string, state: EntryState) {
  if (!role.value) return;
  error.value = null;
  pending.value = permission;
  try {
    await roles.setEntry(role.value.id, permission, state, askFirst.ask, {
      id: props.roomId,
      name: props.roomName,
    });
  } catch (cause) {
    error.value = isApiError(cause) ? cause.message : $ts("roles.notChanged");
  } finally {
    pending.value = null;
  }
}
</script>

<template>
  <section class="mt-5 border-t pt-4">
    <h3 class="text-xs font-semibold">{{ $t("roles.channel.title") }}</h3>
    <p class="mt-1 text-xs text-muted-foreground">{{ $t("roles.channel.hint") }}</p>

    <ul v-if="here.length" class="mt-2 flex flex-wrap gap-1">
      <li
        v-for="entry in here"
        :key="`${entry.role}:${entry.permission}`"
        class="rounded px-1.5 py-0.5 text-[0.6875rem]"
        :class="
          entry.granted ? 'bg-success/15 text-success-text' : 'bg-destructive/15 text-destructive'
        "
      >
        {{ entry.role }} · <code class="font-mono">{{ entry.permission }}</code>
        <span class="sr-only">
          {{ entry.granted ? $t("roles.state.grant") : $t("roles.state.deny") }}
        </span>
      </li>
    </ul>

    <p v-if="error" class="mt-2 text-xs text-destructive" role="alert">{{ error }}</p>

    <div v-if="roles.roles.value.length" class="mt-3">
      <Select
        v-model="chosen"
        class="h-8"
        :label="$ts('roles.channel.whichRole')"
        :options="
          roles.roles.value.map((candidate) => ({ value: candidate.id, label: candidate.name }))
        "
      />
    </div>
    <p
      v-else-if="roles.loading.value"
      class="mt-3 flex items-center gap-2 text-xs text-muted-foreground"
    >
      <Spinner size="sm" />
      {{ $t("roles.loading") }}
    </p>

    <div v-if="role" class="mt-2 space-y-3">
      <div v-for="section in sections" :key="section.group">
        <p class="text-[0.6875rem] font-semibold tracking-wide text-muted-foreground uppercase">
          {{ groupTitle(section.group) }}
        </p>
        <ul class="mt-1 divide-y rounded-lg border">
          <li
            v-for="definition in section.permissions"
            :key="definition.name"
            class="flex items-center gap-2 px-2.5 py-1.5"
          >
            <span class="min-w-0 flex-1">
              <span class="block text-xs">{{
                describe(definition.name, definition.description)
              }}</span>
              <span class="block text-[0.6875rem] text-muted-foreground">
                <code class="font-mono">{{ definition.name }}</code> ·
                {{ serverWide(definition.name) }}
              </span>
            </span>
            <Spinner v-if="pending === definition.name" size="sm" />
            <RolesEntryToggle
              :model-value="entryState(role, definition.name, roomId)"
              :label="
                $ts('roles.channel.entryLabel', { permission: definition.name, role: role.name })
              "
              @update:model-value="(state: EntryState) => set(definition.name, state)"
            />
          </li>
        </ul>
      </div>
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
  </section>
</template>
