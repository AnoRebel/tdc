<script setup lang="ts">
import { useForm, type AnyFieldApi } from "@tanstack/vue-form";
import * as v from "valibot";
import { ArrowDownToLine, Copy, Trash2 } from "lucide-vue-next";

import type { CreatedIncomingWebhook, IncomingWebhook, Room } from "@tdc/types";
import { isApiError } from "@tdc/types";
import { Button } from "#layers/ui/app/components/ui/button";
import { Field } from "#layers/ui/app/components/ui/field";
import { Input } from "#layers/ui/app/components/ui/input";
import { Select } from "#layers/ui/app/components/ui/select";
import { Spinner } from "#layers/ui/app/components/ui/spinner";
import { howLongAgo, onDayAtTime } from "#layers/ui/app/lib/dates";

/**
 * Incoming webhooks: addresses outside systems post into a channel through.
 *
 * Per channel, because that is what one is bound to and who may manage it is
 * decided per channel. The address is shown once, when it is made, and said
 * to be — it is a credential somebody is about to paste into another system,
 * and the moment of making it is the only moment to copy it.
 */

const { $t, $ts } = useI18n();
const integrations = useIntegrations();
const rooms = useRooms();

const channels = ref<Room[]>([]);
const roomId = ref<string | undefined>();
const hooks = ref<IncomingWebhook[]>([]);
const loading = ref(false);
const error = ref<string | null>(null);
/** Whether this person may manage the chosen channel's webhooks at all. */
const allowed = ref(true);

const created = ref<CreatedIncomingWebhook | null>(null);
const { copy, copied } = useClipboard({ legacy: true });
const revoking = ref<IncomingWebhook | null>(null);

onMounted(async () => {
  try {
    const visible = await rooms.list();
    channels.value = visible.filter((room) => room.kind === "channel" && !room.archived);
  } catch (cause) {
    error.value = isApiError(cause) ? cause.message : $ts("integrations.couldNotLoad");
  }
});

async function load() {
  if (!roomId.value) return;
  loading.value = true;
  error.value = null;
  try {
    hooks.value = await integrations.incoming(roomId.value);
    allowed.value = true;
  } catch (cause) {
    hooks.value = [];
    // Not an error to report as one: most people may not manage most
    // channels' webhooks, and the page says so plainly.
    allowed.value = !(isApiError(cause) && cause.status === 403);
    if (allowed.value) {
      error.value = isApiError(cause) ? cause.message : $ts("integrations.couldNotLoad");
    }
  } finally {
    loading.value = false;
  }
}

watch(roomId, () => {
  created.value = null;
  void load();
});

const schema = v.object({
  label: v.pipe(
    v.string(),
    v.trim(),
    v.minLength(1, $ts("integrations.labelRequired")),
    v.maxLength(80, $ts("integrations.labelTooLong")),
  ),
});

const form = useForm({
  defaultValues: { label: "" },
  validators: { onSubmit: schema },
  async onSubmit({ value }) {
    if (!roomId.value) return;
    error.value = null;
    try {
      created.value = await integrations.createIncoming(roomId.value, value.label.trim());
      form.reset();
      await load();
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

async function confirmRevoke() {
  const hook = revoking.value;
  revoking.value = null;
  if (!hook || !roomId.value) return;
  try {
    await integrations.revokeIncoming(roomId.value, hook.id);
    await load();
  } catch (cause) {
    error.value = isApiError(cause) ? cause.message : $ts("integrations.couldNotSave");
  }
}
</script>

<template>
  <section aria-labelledby="incoming-webhooks-title" class="space-y-4">
    <div>
      <h2 id="incoming-webhooks-title" class="flex items-center gap-1.5 text-sm font-semibold">
        <ArrowDownToLine class="size-4" aria-hidden="true" />
        {{ $t("integrations.incomingWebhooks") }}
      </h2>
      <p class="mt-1 text-xs text-muted-foreground">{{ $t("integrations.incomingHint") }}</p>
    </div>

    <Field :label="$ts('integrations.channel')">
      <template #default="{ id }">
        <Select
          :id="id"
          v-model="roomId"
          class="max-w-xs"
          :label="$ts('integrations.channel')"
          :placeholder="$ts('integrations.chooseChannel')"
          :options="channels.map((room) => ({ value: room.id, label: `#${room.name}` }))"
        />
      </template>
    </Field>

    <p
      v-if="error"
      class="rounded-md bg-destructive/10 px-3 py-2 text-sm text-destructive"
      role="alert"
    >
      {{ error }}
    </p>

    <template v-if="roomId">
      <div v-if="loading" class="flex items-center gap-2 text-xs text-muted-foreground">
        <Spinner size="sm" />
        {{ $t("integrations.loading") }}
      </div>

      <p v-else-if="!allowed" class="text-xs text-muted-foreground">
        {{ $t("integrations.cannotManageIncoming") }}
      </p>

      <template v-else>
        <!-- Shown once, and said so. -->
        <div
          v-if="created"
          class="rounded-md border border-primary/40 bg-primary/5 p-3"
          role="status"
        >
          <p class="text-sm font-medium">
            {{ $t("integrations.addressShownOnce", { label: created.label }) }}
          </p>
          <div class="mt-2 flex items-center gap-2">
            <code
              class="min-w-0 flex-1 truncate rounded bg-muted px-2 py-1 font-mono text-xs select-all"
              >{{ created.url }}</code
            >
            <Button variant="outline" size="small" @click="copy(created.url)">
              <Copy class="size-3.5" aria-hidden="true" />
              {{ copied ? $t("integrations.copied") : $t("integrations.copy") }}
            </Button>
            <Button variant="ghost" size="small" @click="created = null">
              {{ $t("integrations.done") }}
            </Button>
          </div>
        </div>

        <p v-if="!hooks.length" class="text-xs text-muted-foreground">
          {{ $t("integrations.noIncomingYet") }}
        </p>
        <ul v-else class="space-y-2">
          <li
            v-for="hook in hooks"
            :key="hook.id"
            class="flex flex-wrap items-start justify-between gap-2 rounded-md border p-3"
            :data-incoming="hook.label"
          >
            <div class="min-w-0 text-xs text-muted-foreground">
              <p class="flex items-center gap-2 text-sm font-medium text-foreground">
                {{ hook.label }}
                <span
                  v-if="hook.revoked"
                  class="rounded-full border border-destructive px-1.5 py-0.5 text-[0.6875rem]"
                >
                  {{ $t("integrations.revoked") }}
                </span>
              </p>
              <p class="mt-0.5">
                {{ $t("integrations.postedCount", { count: hook.posted }) }}
                <template v-if="hook.lastUsed">
                  · {{ $t("integrations.lastUsed", { when: howLongAgo(hook.lastUsed) }) }}
                </template>
              </p>
              <p v-if="hook.refused > 0 && hook.lastRefused" class="mt-0.5">
                {{
                  $t("integrations.refusedCount", {
                    count: hook.refused,
                    when: howLongAgo(hook.lastRefused),
                  })
                }}
              </p>
              <p v-if="hook.revoked" class="mt-0.5">
                {{ $t("integrations.revokedWhen", { when: onDayAtTime(hook.revoked) }) }}
              </p>
            </div>
            <Button v-if="!hook.revoked" variant="ghost" size="small" @click="revoking = hook">
              <Trash2 class="size-3.5" aria-hidden="true" />
              {{ $t("integrations.revoke") }}
            </Button>
          </li>
        </ul>

        <form
          class="flex flex-wrap items-end gap-2"
          :aria-label="$ts('integrations.newIncoming')"
          @submit.prevent="form.handleSubmit()"
        >
          <form.Field name="label">
            <template #default="{ field }">
              <Field
                :label="$ts('integrations.newIncomingLabel')"
                :error="firstError(field)"
                class="min-w-56 flex-1"
                required
              >
                <template #default="{ id, invalid, describedBy }">
                  <Input
                    :id="id"
                    :model-value="field.state.value"
                    :placeholder="$ts('integrations.incomingLabelPlaceholder')"
                    :invalid="invalid"
                    :described-by="describedBy"
                    @update:model-value="field.handleChange($event)"
                    @blur="field.handleBlur"
                  />
                </template>
              </Field>
            </template>
          </form.Field>
          <Button type="submit">{{ $t("integrations.createIncoming") }}</Button>
        </form>
      </template>
    </template>

    <ConfirmDialog
      v-if="revoking"
      :title="$ts('integrations.revokeThisIncoming')"
      :detail="$ts('integrations.revokeIncomingDetail', { label: revoking.label })"
      :confirm-label="$ts('integrations.revoke')"
      :cancel-label="$ts('action.cancel')"
      @cancel="revoking = null"
      @confirm="confirmRevoke"
    />
  </section>
</template>
