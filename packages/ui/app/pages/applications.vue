<script setup lang="ts">
import { ArrowLeft, Copy, KeyRound, Plus, ShieldCheck, Trash2 } from "lucide-vue-next";

import type {
  Application,
  GrantablePermission,
  IssuedApplicationCredential,
  Room,
} from "@tdc/types";
import { isApiError } from "@tdc/types";
import { Button } from "#layers/ui/app/components/ui/button";
import { Checkbox } from "#layers/ui/app/components/ui/checkbox";
import { Spinner } from "#layers/ui/app/components/ui/spinner";
import { howLongAgo, onDayAtTime } from "#layers/ui/app/lib/dates";

/**
 * Applications, as somebody allowed to manage them sees them.
 *
 * Everything an administrator would otherwise reach for curl to do: register,
 * approve having been shown exactly what is granted, choose the channels, issue
 * and revoke credentials, and remove — deciding what happens to what it posted.
 * The server checks the permission on every request; the page only decides
 * what to show somebody who lacks it.
 */

definePageMeta({ middleware: "auth", layout: "app" });

const { $t, $ts, $localeRoute: localeRoute } = useI18n();
useHead({ title: $ts("integrations.applications") });

const integrations = useIntegrations();
const rooms = useRooms();

const listed = ref<Application[]>([]);
const grantable = ref<GrantablePermission[]>([]);
const channels = ref<Room[]>([]);
const loading = ref(true);
const refused = ref(false);
const error = ref<string | null>(null);

/** One application opened for its channels and credentials. */
const open = ref<Application | null>(null);
const editing = ref<string[]>([]);

const approving = ref<Application | null>(null);
const removing = ref<Application | null>(null);
/** A credential just issued — the only time it is ever shown. */
const issued = ref<{ name: string; credential: IssuedApplicationCredential } | null>(null);
const { copy, copied } = useClipboard({ legacy: true });

async function refresh() {
  error.value = null;
  try {
    const [applications, permissions, visible] = await Promise.all([
      integrations.applications(),
      integrations.grantable(),
      rooms.list(),
    ]);
    listed.value = applications;
    grantable.value = permissions;
    channels.value = visible.filter((room) => room.kind === "channel" && !room.archived);
  } catch (cause) {
    refused.value = isApiError(cause) && cause.status === 403;
    if (!refused.value) {
      error.value = isApiError(cause) ? cause.message : $ts("integrations.couldNotLoad");
    }
  } finally {
    loading.value = false;
  }
}

onMounted(() => void refresh());

async function openDetail(application: Application) {
  if (open.value?.id === application.id) {
    open.value = null;
    return;
  }
  try {
    open.value = await integrations.application(application.id);
    editing.value = open.value.channels.map((channel) => channel.id);
  } catch (cause) {
    error.value = isApiError(cause) ? cause.message : $ts("integrations.couldNotLoad");
  }
}

async function approve(chosen: string[]) {
  const application = approving.value;
  approving.value = null;
  if (!application) return;
  error.value = null;
  try {
    const approved = await integrations.approve(application.id, application.requested, chosen);
    issued.value = { name: application.name, credential: approved.credential };
    await refresh();
  } catch (cause) {
    error.value = isApiError(cause) ? cause.message : $ts("integrations.couldNotSave");
  }
}

async function saveChannels() {
  if (!open.value) return;
  error.value = null;
  try {
    open.value = await integrations.restrict(open.value.id, editing.value);
    await refresh();
  } catch (cause) {
    error.value = isApiError(cause) ? cause.message : $ts("integrations.couldNotSave");
  }
}

async function newCredential() {
  if (!open.value) return;
  try {
    const credential = await integrations.issueCredential(open.value.id);
    issued.value = { name: open.value.name, credential };
    open.value = await integrations.application(open.value.id);
  } catch (cause) {
    error.value = isApiError(cause) ? cause.message : $ts("integrations.couldNotSave");
  }
}

async function revokeCredential(id: string) {
  if (!open.value) return;
  try {
    await integrations.revokeCredential(open.value.id, id);
    open.value = await integrations.application(open.value.id);
  } catch (cause) {
    error.value = isApiError(cause) ? cause.message : $ts("integrations.couldNotSave");
  }
}

async function remove(content: "retain" | "remove") {
  const application = removing.value;
  removing.value = null;
  if (!application) return;
  try {
    await integrations.remove(application.id, content);
    if (open.value?.id === application.id) open.value = null;
    await refresh();
  } catch (cause) {
    error.value = isApiError(cause) ? cause.message : $ts("integrations.couldNotSave");
  }
}

function toggleChannel(id: string, on: boolean) {
  editing.value = on ? [...editing.value, id] : editing.value.filter((one) => one !== id);
}
</script>

<template>
  <div class="flex-1 overflow-y-auto">
    <header class="flex h-12 shrink-0 items-center gap-3 border-b px-4">
      <NuxtLink
        :to="localeRoute({ name: 'settings' })"
        class="btn btn-ghost size-7 px-0"
        :aria-label="$ts('integrations.backToSettings')"
      >
        <ArrowLeft class="size-4" aria-hidden="true" />
      </NuxtLink>
      <h1 class="text-sm font-semibold">{{ $t("integrations.applications") }}</h1>
    </header>

    <div class="mx-auto max-w-3xl space-y-6 px-4 py-6">
      <div v-if="loading" class="flex items-center gap-2 text-xs text-muted-foreground">
        <Spinner size="sm" />
        {{ $t("integrations.loading") }}
      </div>

      <p v-else-if="refused" class="text-sm text-muted-foreground">
        {{ $t("integrations.cannotManageApplications") }}
      </p>

      <template v-else>
        <p
          v-if="error"
          class="rounded-md bg-destructive/10 px-3 py-2 text-sm text-destructive"
          role="alert"
        >
          {{ error }}
        </p>

        <div
          v-if="issued"
          class="rounded-md border border-primary/40 bg-primary/5 p-3"
          role="status"
        >
          <p class="text-sm font-medium">
            {{ $t("integrations.credentialShownOnce", { name: issued.name }) }}
          </p>
          <div class="mt-2 flex items-center gap-2">
            <code
              class="min-w-0 flex-1 truncate rounded bg-muted px-2 py-1 font-mono text-xs select-all"
              >{{ issued.credential.token }}</code
            >
            <Button variant="outline" size="small" @click="copy(issued.credential.token)">
              <Copy class="size-3.5" aria-hidden="true" />
              {{ copied ? $t("integrations.copied") : $t("integrations.copy") }}
            </Button>
            <Button variant="ghost" size="small" @click="issued = null">
              {{ $t("integrations.done") }}
            </Button>
          </div>
        </div>

        <p v-if="!listed.length" class="text-xs text-muted-foreground">
          {{ $t("integrations.noApplications") }}
        </p>

        <ul class="space-y-2">
          <li
            v-for="application in listed"
            :key="application.id"
            class="rounded-md border p-3"
            :data-application="application.name"
          >
            <div class="flex flex-wrap items-start justify-between gap-2">
              <div class="min-w-0">
                <p class="flex flex-wrap items-center gap-2 text-sm font-medium">
                  {{ application.name }}
                  <span class="rounded-full border px-1.5 py-0.5 text-[0.6875rem]">
                    {{
                      application.state === "approved"
                        ? $t("integrations.approved")
                        : $t("integrations.awaitingApproval")
                    }}
                  </span>
                </p>
                <p class="mt-0.5 text-xs text-muted-foreground">{{ application.purpose }}</p>
                <p class="mt-1 font-mono text-[0.6875rem]">
                  {{
                    (application.state === "approved"
                      ? application.granted
                      : application.requested
                    ).join(", ") || $t("integrations.holdsNothing")
                  }}
                </p>
                <p
                  v-if="application.approved"
                  class="mt-0.5 text-[0.6875rem] text-muted-foreground"
                >
                  {{ $t("integrations.approvedOn", { when: onDayAtTime(application.approved) }) }}
                  <template v-if="application.channels.length">
                    ·
                    {{ application.channels.map((channel) => `#${channel.name}`).join(", ") }}
                  </template>
                </p>
                <p
                  v-if="application.sendsDataExternally"
                  class="mt-0.5 text-[0.6875rem] text-muted-foreground"
                >
                  {{ $t("integrations.sendsDataElsewhere") }}:
                  {{ application.externalDisclosure }}
                </p>
                <p
                  v-if="application.rateLimited > 0 && application.lastRateLimited"
                  class="mt-0.5 text-[0.6875rem] text-muted-foreground"
                >
                  {{
                    $t("integrations.throttledCount", {
                      count: application.rateLimited,
                      when: howLongAgo(application.lastRateLimited),
                    })
                  }}
                </p>
              </div>

              <div class="flex items-center gap-1">
                <Button
                  v-if="application.state === 'registered'"
                  size="small"
                  @click="approving = application"
                >
                  <ShieldCheck class="size-3.5" aria-hidden="true" />
                  {{ $t("integrations.reviewAndApprove") }}
                </Button>
                <Button
                  v-if="application.state === 'approved'"
                  variant="outline"
                  size="small"
                  :aria-expanded="open?.id === application.id"
                  @click="openDetail(application)"
                >
                  <KeyRound class="size-3.5" aria-hidden="true" />
                  {{ $t("integrations.channelsAndCredentials") }}
                </Button>
                <Button
                  variant="ghost"
                  size="icon"
                  :label="$ts('integrations.removeWhom', { name: application.name })"
                  @click="removing = application"
                >
                  <Trash2 class="size-3.5" aria-hidden="true" />
                </Button>
              </div>
            </div>

            <div v-if="open?.id === application.id" class="mt-3 space-y-3 border-t pt-3">
              <form class="space-y-2" @submit.prevent="saveChannels">
                <fieldset>
                  <legend class="mb-1.5 text-xs font-medium">
                    {{ $t("integrations.actsIn") }}
                  </legend>
                  <div class="max-h-40 space-y-1 overflow-y-auto rounded-md border p-2">
                    <!-- eslint-disable-next-line vuejs-accessibility/label-has-for -- the checkbox is a component, which the rule cannot see inside -->
                    <label
                      v-for="room in channels"
                      :key="room.id"
                      class="flex items-center gap-2 text-xs"
                    >
                      <Checkbox
                        :model-value="editing.includes(room.id)"
                        @update:model-value="(on: boolean) => toggleChannel(room.id, on)"
                      />
                      #{{ room.name }}
                    </label>
                  </div>
                </fieldset>
                <Button type="submit" variant="outline" size="small">
                  {{ $t("integrations.saveChannels") }}
                </Button>
              </form>

              <div>
                <h3 class="text-xs font-medium">{{ $t("integrations.credentials") }}</h3>
                <p class="mt-0.5 text-[0.6875rem] text-muted-foreground">
                  {{ $t("integrations.rotationHint") }}
                </p>
                <ul class="mt-2 space-y-1">
                  <li
                    v-for="credential in open.credentials ?? []"
                    :key="credential.id"
                    class="flex items-center justify-between gap-2 text-xs"
                  >
                    <span>
                      {{ credential.label }} ·
                      {{ $t("integrations.issuedOn", { when: onDayAtTime(credential.created) }) }}
                      <template v-if="credential.lastUsed">
                        ·
                        {{ $t("integrations.lastUsed", { when: howLongAgo(credential.lastUsed) }) }}
                      </template>
                    </span>
                    <Button variant="ghost" size="small" @click="revokeCredential(credential.id)">
                      {{ $t("integrations.revoke") }}
                    </Button>
                  </li>
                </ul>
                <Button class="mt-2" variant="outline" size="small" @click="newCredential">
                  <Plus class="size-3.5" aria-hidden="true" />
                  {{ $t("integrations.issueCredential") }}
                </Button>
              </div>

              <IntegrationsInvocationAddress
                :application-id="application.id"
                :current="open.invocationUrl"
                @saved="refresh"
              />
            </div>
          </li>
        </ul>

        <IntegrationsRegisterApplication :grantable="grantable" @registered="refresh" />
      </template>
    </div>

    <IntegrationsApproveApplication
      v-if="approving"
      :application="approving"
      :grantable="grantable"
      :channels="channels"
      @cancel="approving = null"
      @approve="approve"
    />
    <IntegrationsRemoveApplication
      v-if="removing"
      :name="removing.name"
      @cancel="removing = null"
      @remove="remove"
    />
  </div>
</template>
