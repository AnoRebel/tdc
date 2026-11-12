<script setup lang="ts">
import type { ApiToken, IssuedApiToken } from "@tdc/types";
import { isApiError } from "@tdc/types";
import { useForm } from "@tanstack/vue-form";
import * as v from "valibot";
import { Check, Copy, KeyRound, Plus, Trash2 } from "lucide-vue-next";

import { Button } from "#layers/ui/app/components/ui/button";
import { Checkbox } from "#layers/ui/app/components/ui/checkbox";
import { Field } from "#layers/ui/app/components/ui/field";
import { Input } from "#layers/ui/app/components/ui/input";
import { Select } from "#layers/ui/app/components/ui/select";
import { fully, howLongAgo, onDate } from "#layers/ui/app/lib/dates";
import {
  deniedBy,
  expiryFrom,
  groupPermissions,
  lifetimes,
  readAndPost,
} from "#layers/ui/app/lib/tokens";

/**
 * Personal API tokens: a program's way in without holding a password.
 *
 * The screen is built around how the server decides what a token may do. A
 * token is this account with everything outside what it was left denied — so
 * it starts with nothing ticked, the summary counts what it will be denied,
 * and it can never be more than the account. The secret is shown once, in
 * the moment it is made, because it is stored only as a hash.
 */

const { $t, $ts } = useI18n();
const api = useApi();
const { permissions } = useAuth();

const tokens = ref<ApiToken[]>([]);
const unavailable = ref(false);
const creating = ref(false);
const issued = ref<IssuedApiToken | null>(null);
const copied = ref(false);
const revoking = ref<ApiToken | null>(null);
const error = ref<string | null>(null);

async function load() {
  try {
    tokens.value = await api.get<ApiToken[]>("/api/me/tokens");
  } catch (cause) {
    // A server without tokens answers that it has none to offer; the section
    // then says nothing rather than offering what would be refused.
    unavailable.value = isApiError(cause) && cause.code === "not-found";
  }
}
onMounted(load);

const groups = computed(() => groupPermissions([...permissions.value]));

const lifetimeOptions = computed(() =>
  lifetimes.map((days) => ({
    value: days ?? 0,
    label: days === undefined ? $ts("tokens.never") : $ts("tokens.days", { count: days }),
  })),
);

const schema = computed(() =>
  v.object({
    label: v.pipe(v.string(), v.trim(), v.minLength(1, $ts("tokens.needsName"))),
    lifetime: v.number(),
    scope: v.array(v.string()),
  }),
);

const form = useForm({
  defaultValues: { label: "", lifetime: 30, scope: [] as string[] },
  validators: { onSubmit: schema.value },
  async onSubmit({ value }) {
    error.value = null;
    try {
      issued.value = await api.post<IssuedApiToken>("/api/me/tokens", {
        label: value.label.trim(),
        scope: value.scope,
        expiresAt: expiryFrom(value.lifetime || undefined)?.toISOString(),
      });
      copied.value = false;
      creating.value = false;
      form.reset();
      await load();
    } catch (cause) {
      error.value = isApiError(cause) ? cause.message : $ts("tokens.couldNotCreate");
    }
  },
});

function toggle(scope: string[], permission: string): string[] {
  return scope.includes(permission)
    ? scope.filter((p) => p !== permission)
    : [...scope, permission];
}

async function copy() {
  if (!issued.value) return;
  try {
    await navigator.clipboard.writeText(issued.value.token);
    copied.value = true;
  } catch {
    // No clipboard here; the secret is on screen and selectable.
  }
}

/*
 * Taken off the list at once and put back if the server refuses, so the list
 * never shows a token as gone that still works.
 */
async function confirmRevoke() {
  const token = revoking.value;
  revoking.value = null;
  if (!token) return;
  const before = tokens.value;
  tokens.value = before.filter((t) => t.id !== token.id);
  // A secret for a token that no longer works is not worth keeping on screen.
  if (issued.value?.id === token.id) issued.value = null;
  try {
    await api.delete(`/api/me/tokens/${token.id}`);
  } catch {
    tokens.value = before;
    error.value = $ts("tokens.couldNotRevoke");
  }
}

function firstError(errors: unknown[]): string | undefined {
  const [issue] = errors;
  if (!issue) return undefined;
  return typeof issue === "string" ? issue : (issue as { message?: string }).message;
}
</script>

<template>
  <section v-if="!unavailable" data-api-tokens>
    <h2 class="flex items-center gap-1.5 text-sm font-semibold">
      <KeyRound class="size-4" />
      {{ $t("tokens.title") }}
    </h2>
    <p class="mt-1 text-xs text-muted-foreground">{{ $t("tokens.hint") }}</p>

    <p v-if="error" role="alert" class="mt-2 text-xs text-destructive">{{ error }}</p>

    <!-- The secret, the only time it exists outside a hash. -->
    <Transition name="motion-pop">
      <div
        v-if="issued"
        class="mt-3 rounded-lg border border-warning/50 bg-warning/10 p-3"
        role="status"
      >
        <p class="text-sm font-medium">{{ $t("tokens.issued", { name: issued.label }) }}</p>
        <p class="mt-0.5 text-xs text-muted-foreground">{{ $t("tokens.shownOnce") }}</p>
        <div class="mt-2 flex items-center gap-2">
          <code
            class="min-w-0 flex-1 truncate rounded-md bg-background px-2 py-1.5 font-mono text-xs select-all"
            data-token-secret
          >
            {{ issued.token }}
          </code>
          <Button variant="outline" @click="copy">
            <component :is="copied ? Check : Copy" class="size-3.5" aria-hidden="true" />
            {{ copied ? $t("action.copied") : $t("action.copy") }}
          </Button>
        </div>
        <Button variant="ghost" class="mt-2" @click="issued = null">
          {{ $t("tokens.storedIt") }}
        </Button>
      </div>
    </Transition>

    <ul v-if="tokens.length" class="mt-3 space-y-1.5">
      <TransitionGroup name="motion-list">
        <li
          v-for="token in tokens"
          :key="token.id"
          class="flex flex-wrap items-start justify-between gap-3 rounded-lg border bg-surface px-3 py-2.5"
        >
          <div class="min-w-0">
            <p class="truncate text-sm font-medium">{{ token.label }}</p>
            <p class="mt-0.5 text-xs text-muted-foreground">
              {{
                token.scope.length
                  ? $t("tokens.mayOnly", { scope: token.scope.join(", ") })
                  : $t("tokens.mayNothing")
              }}
            </p>
            <p class="mt-0.5 text-[0.6875rem] text-muted-foreground">
              <time :datetime="token.created" :title="fully(token.created)">
                {{ $t("tokens.created", { when: onDate(token.created) }) }}
              </time>
              ·
              {{
                token.lastUsed
                  ? $t("tokens.lastUsed", { when: howLongAgo(token.lastUsed) })
                  : $t("tokens.neverUsed")
              }}
              ·
              {{
                token.expires
                  ? $t("tokens.expires", { when: onDate(token.expires) })
                  : $t("tokens.noExpiry")
              }}
            </p>
          </div>
          <Button variant="danger" @click="revoking = token">
            <Trash2 class="size-3.5" aria-hidden="true" />
            {{ $t("tokens.revoke") }}
          </Button>
        </li>
      </TransitionGroup>
    </ul>
    <p v-else-if="!creating" class="mt-3 text-xs text-muted-foreground">
      {{ $t("tokens.none") }}
    </p>

    <Button v-if="!creating" variant="outline" class="mt-3" @click="creating = true">
      <Plus class="size-3.5" aria-hidden="true" />
      {{ $t("tokens.create") }}
    </Button>

    <form
      v-else
      class="mt-3 space-y-4 rounded-lg border p-3"
      data-token-form
      @submit.prevent="form.handleSubmit()"
    >
      <div class="grid gap-3 sm:grid-cols-2">
        <form.Field name="label">
          <template #default="{ field }">
            <Field :label="$ts('tokens.name')" :error="firstError(field.state.meta.errors)">
              <template #default="{ id, invalid, describedBy }">
                <Input
                  :id="id"
                  :model-value="field.state.value"
                  :placeholder="$ts('tokens.namePlaceholder')"
                  autocomplete="off"
                  :invalid="invalid"
                  :described-by="describedBy"
                  @update:model-value="field.handleChange($event)"
                />
              </template>
            </Field>
          </template>
        </form.Field>

        <form.Field name="lifetime">
          <template #default="{ field }">
            <Field :label="$ts('tokens.expiresAfter')">
              <template #default="{ id }">
                <Select
                  :id="id"
                  :model-value="field.state.value"
                  :label="$ts('tokens.expiresAfter')"
                  :options="lifetimeOptions"
                  @update:model-value="$event !== undefined && field.handleChange($event)"
                />
              </template>
            </Field>
          </template>
        </form.Field>
      </div>

      <form.Field name="scope">
        <template #default="{ field }">
          <fieldset>
            <legend class="text-xs font-medium">{{ $t("tokens.mayDo") }}</legend>
            <p class="mt-0.5 text-xs text-muted-foreground">{{ $t("tokens.denialModel") }}</p>

            <div class="mt-2 flex flex-wrap gap-1.5">
              <Button
                variant="outline"
                class="h-7 px-2 text-xs"
                @click="field.handleChange(readAndPost([...permissions]))"
              >
                {{ $t("tokens.presetReadPost") }}
              </Button>
              <Button variant="ghost" class="h-7 px-2 text-xs" @click="field.handleChange([])">
                {{ $t("tokens.presetNothing") }}
              </Button>
            </div>

            <div class="mt-2 max-h-72 space-y-3 overflow-y-auto pr-1">
              <div v-for="group in groups" :key="group.area">
                <p
                  class="text-[0.6875rem] font-semibold tracking-wide text-muted-foreground uppercase"
                >
                  {{ $t(`tokens.area.${group.area}`) }}
                </p>
                <div class="mt-1 grid gap-0.5 sm:grid-cols-2">
                  <label
                    v-for="permission in group.permissions"
                    :key="permission"
                    class="flex cursor-pointer items-center gap-2 rounded-md px-1.5 py-1 text-xs hover:bg-accent"
                  >
                    <Checkbox
                      :model-value="field.state.value.includes(permission)"
                      @update:model-value="
                        field.handleChange(toggle(field.state.value, permission))
                      "
                    />
                    <code class="font-mono">{{ permission }}</code>
                  </label>
                </div>
              </div>
            </div>

            <!-- What the choice amounts to, in the server's own terms. -->
            <p class="mt-2 text-xs" aria-live="polite">
              {{
                $t("tokens.summary", {
                  kept: field.state.value.length,
                  denied: deniedBy([...permissions], field.state.value).length,
                })
              }}
            </p>
          </fieldset>
        </template>
      </form.Field>

      <p class="text-xs text-muted-foreground">{{ $t("tokens.cannotMint") }}</p>

      <div class="flex gap-2">
        <Button type="submit">{{ $t("tokens.create") }}</Button>
        <Button variant="ghost" @click="creating = false">{{ $t("action.cancel") }}</Button>
      </div>
    </form>

    <ConfirmDialog
      v-if="revoking"
      :title="$ts('tokens.revokeTitle', { name: revoking.label })"
      :detail="$ts('tokens.revokeDetail')"
      :confirm-label="$ts('tokens.revoke')"
      :cancel-label="$ts('action.cancel')"
      @confirm="confirmRevoke"
      @cancel="revoking = null"
    />
  </section>
</template>
