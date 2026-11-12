<script setup lang="ts">
import { useForm, type AnyFieldApi } from "@tanstack/vue-form";
import * as v from "valibot";
import {
  ChevronDown,
  ChevronRight,
  Copy,
  Power,
  ShieldAlert,
  Trash2,
  TriangleAlert,
  Webhook,
} from "lucide-vue-next";

import type {
  CreatedWebhook,
  Project,
  Room,
  WebhookEventType,
  WebhookHistory,
  WebhookScope,
  WebhookSubscription,
} from "@tdc/types";
import { isApiError } from "@tdc/types";
import { Button } from "#layers/ui/app/components/ui/button";
import { Checkbox } from "#layers/ui/app/components/ui/checkbox";
import { Field } from "#layers/ui/app/components/ui/field";
import { Input } from "#layers/ui/app/components/ui/input";
import { Select } from "#layers/ui/app/components/ui/select";
import { Spinner } from "#layers/ui/app/components/ui/spinner";
import { howLongAgo, onDayAtTime } from "#layers/ui/app/lib/dates";

/**
 * Outgoing webhooks: an endpoint told about a channel's, a project's or the
 * whole server's events as they happen.
 *
 * Everything an owner needs to answer "is it working?" without reading a log:
 * whether the subscription is on, how many attempts have failed since the last
 * success, whether a rate limit held anything back, and — one click away —
 * every recent attempt with its outcome and the status the receiver answered.
 */

const { $t, $ts } = useI18n();
const integrations = useIntegrations();
const rooms = useRooms();
const projectsApi = useProjects();

const subscriptions = ref<WebhookSubscription[]>([]);
const channels = ref<Room[]>([]);
const projects = ref<Project[]>([]);
const catalogue = ref<WebhookEventType[]>([]);
const loading = ref(true);
const error = ref<string | null>(null);

/** The one webhook whose history is open, with what was fetched for it. */
const open = ref<string | null>(null);
const history = ref<WebhookHistory | null>(null);
const loadingHistory = ref(false);

/** The secret of a webhook just made — the only time it is ever shown. */
const created = ref<CreatedWebhook | null>(null);
const { copy, copied } = useClipboard({ legacy: true });

const removing = ref<WebhookSubscription | null>(null);

async function refresh() {
  error.value = null;
  try {
    const [owned, visible, types, readable] = await Promise.all([
      integrations.webhooks(),
      rooms.list(),
      integrations.eventTypes(),
      // A server without projects answers with an error; it has none to offer.
      projectsApi.list().catch(() => [] as Project[]),
    ]);
    subscriptions.value = owned;
    // Channels only: a direct conversation is not something to point a
    // program at, and the server refuses nothing it could not read anyway.
    channels.value = visible.filter((room) => room.kind === "channel" && !room.archived);
    projects.value = readable;
    catalogue.value = types.catalogue;
  } catch (cause) {
    error.value = isApiError(cause) ? cause.message : $ts("integrations.couldNotLoad");
  } finally {
    loading.value = false;
  }
}

onMounted(() => void refresh());

/** What a subscription listens to, as a person reads it. */
function targetName(subscription: WebhookSubscription): string {
  if (subscription.scope === "server") return $ts("integrations.wholeServer");
  if (subscription.scope === "project") {
    const project = projects.value.find((candidate) => candidate.id === subscription.projectId);
    return project ? `${project.key} · ${project.name}` : $ts("integrations.aProject");
  }
  const room = channels.value.find((candidate) => candidate.id === subscription.roomId);
  return room ? `#${room.name}` : $ts("integrations.aChannel");
}

/** A type's key under the catalogue's translations: message.sent → message_sent. */
function typeKey(name: string): string {
  return name.replaceAll(".", "_");
}

/** The types that can happen where the form is pointed, in their groups. */
function groupsFor(scope: WebhookScope): { group: string; types: WebhookEventType[] }[] {
  const groups: { group: string; types: WebhookEventType[] }[] = [];
  for (const kind of catalogue.value) {
    if (!kind.scopes.includes(scope)) continue;
    let entry = groups.find((candidate) => candidate.group === kind.group);
    if (!entry) {
      entry = { group: kind.group, types: [] };
      groups.push(entry);
    }
    entry.types.push(kind);
  }
  return groups;
}

const scopeOptions = computed(() => [
  { value: "channel", label: $ts("integrations.scopeChannel") },
  { value: "project", label: $ts("integrations.scopeProject") },
  { value: "server", label: $ts("integrations.scopeServer") },
]);

async function toggleHistory(subscription: WebhookSubscription) {
  if (open.value === subscription.id) {
    open.value = null;
    return;
  }
  open.value = subscription.id;
  history.value = null;
  loadingHistory.value = true;
  try {
    history.value = await integrations.webhook(subscription.id);
  } catch (cause) {
    error.value = isApiError(cause) ? cause.message : $ts("integrations.couldNotLoad");
  } finally {
    loadingHistory.value = false;
  }
}

async function enable(subscription: WebhookSubscription) {
  error.value = null;
  try {
    await integrations.enable(subscription.id);
    await refresh();
  } catch (cause) {
    error.value = isApiError(cause) ? cause.message : $ts("integrations.couldNotSave");
  }
}

async function confirmRemoval() {
  const subscription = removing.value;
  removing.value = null;
  if (!subscription) return;

  error.value = null;
  try {
    await integrations.unsubscribe(subscription.id);
    if (open.value === subscription.id) open.value = null;
    await refresh();
  } catch (cause) {
    error.value = isApiError(cause) ? cause.message : $ts("integrations.couldNotSave");
  }
}

/*
 * The form's rules, stated once. The server checks all of them again — and
 * checks what no client can, whether the address resolves somewhere it will
 * deliver — so these exist to say what is wrong before the round trip.
 */
const schema = v.object({
  label: v.pipe(
    v.string(),
    v.trim(),
    v.minLength(1, $ts("integrations.labelRequired")),
    v.maxLength(80, $ts("integrations.labelTooLong")),
  ),
  scope: v.picklist(["channel", "project", "server"]),
  target: v.string(),
  url: v.pipe(
    v.string(),
    v.trim(),
    v.url($ts("integrations.urlInvalid")),
    v.regex(/^https?:\/\//, $ts("integrations.urlInvalid")),
  ),
  eventTypes: v.pipe(v.array(v.string()), v.minLength(1, $ts("integrations.eventTypeRequired"))),
});

/*
 * What it listens to must be named, unless it is the whole server — said on the
 * field that names it, in the words for what is missing.
 */
const schemaWithTarget = v.pipe(
  schema,
  v.forward(
    v.partialCheck(
      [["scope"], ["target"]],
      (input) => input.scope === "server" || input.target.length > 0,
      $ts("integrations.targetRequired"),
    ),
    ["target"],
  ),
);

const form = useForm({
  defaultValues: {
    label: "",
    scope: "channel" as WebhookScope,
    target: "",
    url: "",
    eventTypes: ["message.sent"] as string[],
  },
  validators: { onSubmit: schemaWithTarget },
  async onSubmit({ value }) {
    error.value = null;
    try {
      created.value = await integrations.subscribe({
        scope: value.scope,
        roomId: value.scope === "channel" ? value.target : undefined,
        projectId: value.scope === "project" ? value.target : undefined,
        label: value.label,
        url: value.url,
        eventTypes: value.eventTypes,
      });
      form.reset();
      await refresh();
    } catch (cause) {
      error.value = isApiError(cause) ? cause.message : $ts("integrations.couldNotSave");
    }
  },
});

function firstError(field: AnyFieldApi): string | undefined {
  if (!field.state.meta.isTouched) return undefined;
  const [issue] = field.state.meta.errors;
  if (!issue) return undefined;
  return typeof issue === "string" ? issue : (issue as { message?: string }).message;
}

/*
 * Pointing the form somewhere else forgets what it was pointed at, and the
 * types that cannot happen there — rather than sending a choice the server
 * would refuse.
 */
function changeScope(scope: WebhookScope) {
  form.setFieldValue("scope", scope);
  form.setFieldValue("target", "");
  const possible = new Set(
    catalogue.value.filter((kind) => kind.scopes.includes(scope)).map((kind) => kind.name),
  );
  form.setFieldValue(
    "eventTypes",
    form.getFieldValue("eventTypes").filter((kind) => possible.has(kind)),
  );
}

function toggleType(field: AnyFieldApi, kind: string, on: boolean) {
  const current = new Set(field.state.value as string[]);
  if (on) current.add(kind);
  else current.delete(kind);
  field.handleChange([...current]);
  field.handleBlur();
}
</script>

<template>
  <section aria-labelledby="outgoing-webhooks-title" class="space-y-4">
    <div>
      <h2 id="outgoing-webhooks-title" class="flex items-center gap-1.5 text-sm font-semibold">
        <Webhook class="size-4" aria-hidden="true" />
        {{ $t("integrations.outgoingWebhooks") }}
      </h2>
      <p class="mt-1 text-xs text-muted-foreground">{{ $t("integrations.outgoingHint") }}</p>
    </div>

    <p
      v-if="error"
      class="rounded-md bg-destructive/10 px-3 py-2 text-sm text-destructive"
      role="alert"
    >
      {{ error }}
    </p>

    <!-- Shown once, and said so, because it is the only chance anybody gets. -->
    <div v-if="created" class="rounded-md border border-primary/40 bg-primary/5 p-3" role="status">
      <p class="text-sm font-medium">
        {{ $t("integrations.secretShownOnce", { label: created.label }) }}
      </p>
      <div class="mt-2 flex items-center gap-2">
        <code
          class="min-w-0 flex-1 truncate rounded bg-muted px-2 py-1 font-mono text-xs select-all"
          >{{ created.secret }}</code
        >
        <Button variant="outline" size="small" @click="copy(created.secret)">
          <Copy class="size-3.5" aria-hidden="true" />
          {{ copied ? $t("integrations.copied") : $t("integrations.copy") }}
        </Button>
        <Button variant="ghost" size="small" @click="created = null">
          {{ $t("integrations.done") }}
        </Button>
      </div>
    </div>

    <div v-if="loading" class="flex items-center gap-2 text-xs text-muted-foreground">
      <Spinner size="sm" />
      {{ $t("integrations.loading") }}
    </div>

    <p v-else-if="!subscriptions.length" class="text-xs text-muted-foreground">
      {{ $t("integrations.noWebhooksYet") }}
    </p>

    <ul v-else class="space-y-2">
      <li
        v-for="subscription in subscriptions"
        :key="subscription.id"
        class="rounded-md border p-3"
        :data-webhook="subscription.label"
      >
        <div class="flex flex-wrap items-start justify-between gap-2">
          <div class="min-w-0">
            <p class="flex flex-wrap items-center gap-2 text-sm font-medium">
              {{ subscription.label }}
              <span
                class="rounded-full border px-1.5 py-0.5 text-[0.6875rem] font-medium"
                :class="subscription.state === 'active' ? 'border-border' : 'border-destructive'"
              >
                {{
                  subscription.state === "active"
                    ? $t("integrations.active")
                    : $t("integrations.turnedOff")
                }}
              </span>
            </p>
            <p class="mt-0.5 truncate font-mono text-xs text-muted-foreground">
              {{ subscription.url }}
            </p>
            <p class="mt-0.5 text-xs text-muted-foreground">
              {{ targetName(subscription) }} · {{ subscription.eventTypes.join(", ") }}
            </p>
          </div>

          <div class="flex items-center gap-1">
            <Button
              v-if="subscription.state === 'disabled'"
              variant="outline"
              size="small"
              @click="enable(subscription)"
            >
              <Power class="size-3.5" aria-hidden="true" />
              {{ $t("integrations.turnBackOn") }}
            </Button>
            <Button
              variant="ghost"
              size="icon"
              :label="$ts('integrations.removeWebhook', { label: subscription.label })"
              @click="removing = subscription"
            >
              <Trash2 class="size-3.5" aria-hidden="true" />
            </Button>
          </div>
        </div>

        <!-- Why it is not delivering, in words, when it is not. -->
        <p
          v-if="subscription.state === 'disabled'"
          class="mt-2 flex items-start gap-1.5 text-xs text-destructive"
        >
          <ShieldAlert class="mt-0.5 size-3.5 shrink-0" aria-hidden="true" />
          {{
            $t("integrations.turnedOffBecause", {
              reason: subscription.disabledReason ?? "",
              when: subscription.disabledAt ? onDayAtTime(subscription.disabledAt) : "",
            })
          }}
        </p>
        <p
          v-else-if="subscription.accessLost"
          class="mt-2 flex items-start gap-1.5 text-xs text-muted-foreground"
        >
          <TriangleAlert class="mt-0.5 size-3.5 shrink-0" aria-hidden="true" />
          {{ $t("integrations.accessLost") }}
        </p>
        <p
          v-else-if="subscription.consecutiveFailures > 0"
          class="mt-2 flex items-start gap-1.5 text-xs text-muted-foreground"
        >
          <TriangleAlert class="mt-0.5 size-3.5 shrink-0" aria-hidden="true" />
          {{ $t("integrations.failingNow", { count: subscription.consecutiveFailures }) }}
        </p>
        <p
          v-if="subscription.rateLimited > 0 && subscription.lastRateLimited"
          class="mt-1 text-xs text-muted-foreground"
        >
          {{
            $t("integrations.heldBackCount", {
              count: subscription.rateLimited,
              when: howLongAgo(subscription.lastRateLimited),
            })
          }}
        </p>

        <Button
          variant="ghost"
          size="small"
          class="mt-2 -ml-2"
          :aria-expanded="open === subscription.id"
          :aria-controls="`history-${subscription.id}`"
          @click="toggleHistory(subscription)"
        >
          <ChevronDown v-if="open === subscription.id" class="size-3.5" aria-hidden="true" />
          <ChevronRight v-else class="size-3.5" aria-hidden="true" />
          {{ $t("integrations.deliveryHistory") }}
        </Button>

        <div v-if="open === subscription.id" :id="`history-${subscription.id}`" class="mt-2">
          <div v-if="loadingHistory" class="flex items-center gap-2 text-xs text-muted-foreground">
            <Spinner size="sm" />
            {{ $t("integrations.loading") }}
          </div>
          <IntegrationsWebhookHistory
            v-else-if="history"
            :attempts="history.attempts"
            :deliveries="history.deliveries"
          />
        </div>
      </li>
    </ul>

    <form
      class="space-y-3 rounded-md border p-3"
      :aria-label="$ts('integrations.newWebhook')"
      @submit.prevent="form.handleSubmit()"
    >
      <h3 class="text-sm font-medium">{{ $t("integrations.newWebhook") }}</h3>

      <form.Field name="label">
        <template #default="{ field }">
          <Field :label="$ts('integrations.label')" :error="firstError(field)" required>
            <template #default="{ id, invalid, describedBy }">
              <Input
                :id="id"
                :model-value="field.state.value"
                :placeholder="$ts('integrations.labelPlaceholder')"
                :invalid="invalid"
                :described-by="describedBy"
                @update:model-value="field.handleChange($event)"
                @blur="field.handleBlur"
              />
            </template>
          </Field>
        </template>
      </form.Field>

      <form.Field name="scope">
        <template #default="{ field }">
          <Field :label="$ts('integrations.listensTo')" :hint="$ts('integrations.listensToHint')">
            <template #default="{ id }">
              <Select
                :id="id"
                :model-value="field.state.value"
                :label="$ts('integrations.listensTo')"
                :options="scopeOptions"
                @update:model-value="(value) => changeScope(value as WebhookScope)"
              />
            </template>
          </Field>
        </template>
      </form.Field>

      <form.Subscribe :selector="(state) => ({ scope: state.values.scope })">
        <template #default="{ scope }">
          <form.Field v-if="scope !== 'server'" name="target">
            <template #default="{ field }">
              <Field
                :label="
                  scope === 'project' ? $ts('integrations.project') : $ts('integrations.channel')
                "
                :error="firstError(field)"
                required
              >
                <template #default="{ id }">
                  <Select
                    :id="id"
                    :model-value="field.state.value || undefined"
                    :label="
                      scope === 'project'
                        ? $ts('integrations.project')
                        : $ts('integrations.channel')
                    "
                    :placeholder="
                      scope === 'project'
                        ? $ts('integrations.chooseProject')
                        : $ts('integrations.chooseChannel')
                    "
                    :options="
                      scope === 'project'
                        ? projects.map((project) => ({
                            value: project.id,
                            label: `${project.key} · ${project.name}`,
                          }))
                        : channels.map((room) => ({ value: room.id, label: `#${room.name}` }))
                    "
                    @update:model-value="
                      (value) => {
                        field.handleChange(String(value ?? ''));
                        field.handleBlur();
                      }
                    "
                  />
                </template>
              </Field>
            </template>
          </form.Field>
        </template>
      </form.Subscribe>

      <form.Field name="url">
        <template #default="{ field }">
          <Field
            :label="$ts('integrations.endpoint')"
            :hint="$ts('integrations.endpointHint')"
            :error="firstError(field)"
            required
          >
            <template #default="{ id, invalid, describedBy }">
              <Input
                :id="id"
                type="url"
                autocomplete="off"
                :model-value="field.state.value"
                placeholder="https://ci.example.org/hooks/tdc"
                :invalid="invalid"
                :described-by="describedBy"
                @update:model-value="field.handleChange($event)"
                @blur="field.handleBlur"
              />
            </template>
          </Field>
        </template>
      </form.Field>

      <form.Field name="eventTypes">
        <template #default="{ field }">
          <fieldset>
            <legend class="mb-1.5 text-sm font-medium">{{ $t("integrations.eventTypes") }}</legend>
            <form.Subscribe :selector="(state) => ({ scope: state.values.scope })">
              <template #default="{ scope }">
                <div class="space-y-3">
                  <fieldset v-for="entry in groupsFor(scope)" :key="entry.group">
                    <legend class="mb-1 text-xs font-medium text-muted-foreground">
                      {{ $t(`integrations.eventGroups.${entry.group}`) }}
                    </legend>
                    <div class="grid grid-cols-1 gap-1.5 sm:grid-cols-2">
                      <!-- eslint-disable-next-line vuejs-accessibility/label-has-for -- the checkbox is a component, which the rule cannot see inside -->
                      <label
                        v-for="kind in entry.types"
                        :key="kind.name"
                        class="flex items-start gap-2 text-xs"
                        :class="kind.available ? '' : 'opacity-60'"
                        :data-event-type="kind.name"
                      >
                        <Checkbox
                          class="mt-0.5"
                          :disabled="!kind.available"
                          :model-value="(field.state.value as string[]).includes(kind.name)"
                          @update:model-value="(on: boolean) => toggleType(field, kind.name, on)"
                        />
                        <span class="min-w-0">
                          <span class="block">{{
                            $t(`integrations.eventDescriptions.${typeKey(kind.name)}`)
                          }}</span>
                          <span class="block font-mono text-[0.6875rem] text-muted-foreground">
                            {{ kind.name }}
                          </span>
                          <span
                            v-if="!kind.available && kind.permission"
                            class="block text-[0.6875rem] text-muted-foreground"
                          >
                            {{
                              $t("integrations.needsPermission", { permission: kind.permission })
                            }}
                          </span>
                        </span>
                      </label>
                    </div>
                  </fieldset>
                </div>
              </template>
            </form.Subscribe>
            <p v-if="firstError(field)" class="mt-1 text-xs text-destructive" role="alert">
              {{ firstError(field) }}
            </p>
          </fieldset>
        </template>
      </form.Field>

      <div class="flex justify-end">
        <form.Subscribe>
          <template #default="{ isSubmitting }">
            <Button type="submit" :disabled="isSubmitting">
              <Spinner v-if="isSubmitting" size="sm" />
              {{ $t("integrations.subscribe") }}
            </Button>
          </template>
        </form.Subscribe>
      </div>
    </form>

    <ConfirmDialog
      v-if="removing"
      :title="$ts('integrations.removeThisWebhook')"
      :detail="$ts('integrations.removeWebhookDetail', { label: removing.label })"
      :confirm-label="$ts('integrations.remove')"
      :cancel-label="$ts('action.cancel')"
      @cancel="removing = null"
      @confirm="confirmRemoval"
    />
  </section>
</template>
