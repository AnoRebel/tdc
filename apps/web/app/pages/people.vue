<script setup lang="ts">
import { useForm } from "@tanstack/vue-form";
import type { AnyFieldApi } from "@tanstack/vue-form";
import * as v from "valibot";

import { isApiError } from "@tdc/types";
import { ArrowLeft, Check, Copy, ShieldAlert, UserPlus, UserX } from "lucide-vue-next";
import { Avatar } from "#layers/ui/app/components/ui/avatar";
import type { Person } from "#layers/ui/app/composables/usePeople";
import { Button } from "#layers/ui/app/components/ui/button";
import { Select } from "#layers/ui/app/components/ui/select";
import { Textarea } from "#layers/ui/app/components/ui/textarea";
import { onDate, onDayAtTime } from "#layers/ui/app/lib/dates";
import { Field } from "#layers/ui/app/components/ui/field";

const { $localeRoute: localeRoute } = useI18n();

/**
 * Who is on this server: suspending somebody, lifting it, and inviting people.
 *
 * The two halves need different permissions, so either can be empty while the
 * other works — somebody who may invite but not suspend sees the invitations
 * and not the list.
 */

definePageMeta({ middleware: "auth", layout: "app" });

useHead({ title: "People" });

const people = usePeople();
const { can } = useAuth();

const error = ref<string | null>(null);
const suspending = ref<Person | null>(null);
/*
 * What a suspension needs: a reason somebody else can read.
 *
 * Required rather than merely encouraged — a suspension nobody can explain is
 * one nobody else can review, which the note under the box already said and
 * nothing enforced.
 */
const suspension = v.object({
  reason: v.pipe(v.string(), v.trim(), v.minLength(1, "Say why, for whoever reviews this.")),
  hours: v.number(),
});

/** The first thing wrong with a field, once somebody has touched it. */
function firstError(field: AnyFieldApi): string | undefined {
  if (!field.state.meta.isTouched) return undefined;
  const [issue] = field.state.meta.errors;
  if (!issue) return undefined;
  return typeof issue === "string" ? issue : (issue as { message?: string }).message;
}
const inviting = ref(false);
const issued = ref<{ url?: string; code?: string } | null>(null);
const copied = ref(false);

onMounted(() => void people.refresh());

/*
 * Durations offered rather than a number typed.
 *
 * Nought is permanent, which is the one that needs saying plainly: a
 * suspension with no end is a different decision from a long one, and somebody
 * choosing it should know they are choosing it.
 */
const durations = [
  { hours: 24, label: "A day" },
  { hours: 168, label: "A week" },
  { hours: 720, label: "A month" },
  { hours: 0, label: "Until somebody lifts it" },
];

/*
 * The invitation somebody is withdrawing, held until they confirm.
 *
 * Withdrawing cannot be undone and is not obviously destructive from the
 * button — "Withdraw" beside a list reads like tidying up, and whoever holds
 * the link simply finds it stops working.
 */
const withdrawing = ref<ServerInvitation | null>(null);

async function confirmWithdrawal() {
  const invitation = withdrawing.value;
  if (!invitation) return;

  withdrawing.value = null;
  await people.revoke(invitation.id);
}

const suspendForm = useForm({
  defaultValues: { reason: "", hours: 0 },
  validators: { onSubmit: suspension },
  async onSubmit({ value }) {
    const person = suspending.value;
    if (!person) return;

    suspending.value = null;
    error.value = null;

    try {
      await people.suspend(person.id, value.reason, value.hours || undefined);
      suspendForm.reset();
    } catch (cause) {
      error.value = isApiError(cause) ? cause.message : "That suspension was not accepted.";
    }
  },
});

/** Opens the suspension form for one person, cleared and set to a day. */
function askToSuspend(person: (typeof people.people.value)[number]) {
  suspending.value = person;
  suspendForm.reset();
  suspendForm.setFieldValue("hours", 24);
}

async function lift(person: Person) {
  error.value = null;
  try {
    await people.lift(person.id);
  } catch (cause) {
    error.value = isApiError(cause) ? cause.message : "That suspension could not be lifted.";
  }
}

async function createInvitation() {
  inviting.value = false;
  error.value = null;

  try {
    const invitation = await people.invite({ expiresInHours: 168, withCode: true });
    // Shown once, because the server returns the link and code only when they
    // are issued — afterwards it holds a hash and cannot show them again.
    issued.value = { url: invitation.url, code: invitation.code };
  } catch (cause) {
    error.value = isApiError(cause) ? cause.message : "That invitation was not issued.";
  }
}

async function copyInvitation() {
  if (!issued.value?.url) return;

  await navigator.clipboard?.writeText(issued.value.url);
  copied.value = true;
  setTimeout(() => (copied.value = false), 2000);
}

/** When a suspension lifts, in words rather than a timestamp. */
function until(person: Person): string {
  if (!person.suspendedUntil) return "Until somebody lifts it";

  return `Until ${onDayAtTime(person.suspendedUntil)}`;
}

function when(value: string): string {
  return onDate(value);
}
</script>

<template>
  <div class="flex-1 overflow-y-auto">
    <header class="flex h-12 shrink-0 items-center gap-3 border-b px-4">
      <NuxtLink
        :to="localeRoute({ name: 'settings' })"
        class="btn btn-ghost size-7 px-0"
        aria-label="Back to settings"
      >
        <ArrowLeft class="size-4" />
      </NuxtLink>
      <h1 class="text-sm font-semibold">People</h1>
    </header>

    <div class="max-w-3xl space-y-8 px-4 py-6">
      <div
        v-if="people.failed.value"
        class="flex gap-3 rounded-lg border border-destructive/30 bg-destructive/8 p-4"
        role="alert"
      >
        <ShieldAlert class="mt-0.5 size-4 shrink-0 text-destructive" />
        <div class="text-sm">
          <p class="font-medium text-destructive">You cannot manage people on this server.</p>
          <p class="mt-1 text-muted-foreground">
            This needs the
            <code class="rounded bg-muted px-1 py-0.5 font-mono text-xs">user.suspend</code>
            or
            <code class="rounded bg-muted px-1 py-0.5 font-mono text-xs">user.invite</code>
            permission.
          </p>
        </div>
      </div>

      <template v-else>
        <p
          v-if="error"
          class="rounded-md bg-destructive/10 px-3 py-2 text-sm text-destructive"
          role="alert"
        >
          {{ error }}
        </p>

        <section v-if="can('user.invite')">
          <div class="flex items-start justify-between gap-4">
            <div>
              <h2 class="text-sm font-semibold">Invitations</h2>
              <p class="mt-0.5 text-xs text-muted-foreground">
                A link, and a short code for reading aloud. Both last a week.
              </p>
            </div>
            <Button class="shrink-0" @click="createInvitation">
              <UserPlus class="size-3.5" />
              Invite
            </Button>
          </div>

          <!-- Shown once. The server keeps a hash, so it cannot show this
               again and saying so is more honest than a page that looks like
               it could. -->
          <div v-if="issued" class="mt-3 rounded-lg border border-primary/30 bg-primary/8 p-3">
            <p class="text-xs font-medium">Share this now — it is not shown again.</p>
            <div class="mt-2 flex flex-wrap items-center gap-2">
              <code class="min-w-0 flex-1 truncate rounded bg-surface px-2 py-1 font-mono text-xs">
                {{ issued.url }}
              </code>
              <Button variant="outline" class="shrink-0" @click="copyInvitation">
                <component :is="copied ? Check : Copy" class="size-3.5" />
                {{ copied ? "Copied" : "Copy" }}
              </Button>
            </div>
            <p v-if="issued.code" class="mt-2 text-xs text-muted-foreground">
              Or the code <span class="font-mono font-semibold">{{ issued.code }}</span>
            </p>
          </div>

          <p v-if="!people.invitations.value.length" class="mt-3 text-xs text-muted-foreground">
            No invitations outstanding.
          </p>

          <ul v-else class="mt-3 space-y-1">
            <li
              v-for="invitation in people.invitations.value"
              :key="invitation.id"
              class="flex items-center gap-3 rounded-lg px-2 py-2 transition-colors hover:bg-accent"
            >
              <span class="min-w-0 flex-1">
                <span class="block text-sm">
                  {{ invitation.boundTo || "Anybody with the link" }}
                  <span
                    v-if="!invitation.usable"
                    class="ml-1.5 rounded bg-muted px-1.5 py-0.5 text-[0.6875rem] text-muted-foreground"
                  >
                    {{ invitation.revokedAt ? "Withdrawn" : "Expired" }}
                  </span>
                </span>
                <span class="block text-xs text-muted-foreground">
                  Used {{ invitation.uses }}
                  <template v-if="invitation.maxUses">of {{ invitation.maxUses }}</template>
                  times · issued {{ when(invitation.createdAt) }}
                </span>
              </span>
              <Button
                variant="ghost"
                class="h-7 px-2 text-xs"
                v-if="invitation.usable"
                @click="withdrawing = invitation"
              >
                Withdraw
              </Button>
            </li>
          </ul>
        </section>

        <section v-if="can('user.suspend')">
          <h2 class="text-sm font-semibold">Everybody here</h2>
          <p class="mt-0.5 text-xs text-muted-foreground">
            Suspending somebody ends their sessions and stops them signing in. Their messages stay.
          </p>

          <ul class="mt-3 space-y-1">
            <li
              v-for="person in people.people.value"
              :key="person.id"
              class="flex items-center gap-2.5 rounded-lg px-2 py-2 transition-colors hover:bg-accent"
            >
              <Avatar :name="person.displayName || person.username" size="sm" />
              <span class="min-w-0 flex-1">
                <span class="block truncate text-sm font-medium">
                  {{ person.displayName || person.username }}
                  <span
                    v-if="person.suspended"
                    class="ml-1.5 rounded bg-destructive/15 px-1.5 py-0.5 text-[0.6875rem] font-medium text-destructive"
                  >
                    Suspended
                  </span>
                </span>
                <span class="block truncate text-xs text-muted-foreground">
                  {{ person.address }}
                  <template v-if="person.suspended"> · {{ until(person) }}</template>
                </span>
                <span
                  v-if="person.suspended && person.suspensionNote"
                  class="block truncate text-xs text-muted-foreground italic"
                >
                  {{ person.suspensionNote }}
                </span>
              </span>

              <Button
                variant="ghost"
                class="h-7 shrink-0 px-2 text-xs"
                v-if="person.suspended"
                @click="lift(person)"
              >
                Lift
              </Button>
              <Button
                variant="ghost"
                class="h-7 shrink-0 px-2 text-xs text-destructive"
                v-else
                @click="askToSuspend(person)"
              >
                <UserX class="size-3.5" />
                Suspend
              </Button>
            </li>
          </ul>
        </section>
      </template>
    </div>

    <div
      v-if="suspending"
      class="fixed inset-0 z-50 flex items-center justify-center bg-background/70 p-4 backdrop-blur-sm"
      role="dialog"
      aria-modal="true"
      aria-label="Suspend this account"
      @click.self="suspending = null"
      @keydown.esc="suspending = null"
    >
      <div class="w-[min(26rem,100%)] rounded-xl border border-border bg-card p-4 shadow-2xl">
        <h2 class="text-sm font-semibold">
          Suspend {{ suspending.displayName || suspending.username }}
        </h2>
        <p class="mt-1 text-xs text-muted-foreground">
          Their sessions end at once and they cannot sign in. What they have written stays where it
          is.
        </p>

        <form class="mt-3 space-y-3" @submit.prevent="suspendForm.handleSubmit()">
          <suspendForm.Field name="reason">
            <template #default="{ field }">
              <Field
                label="Why"
                hint="Recorded, and shown to whoever reviews this later."
                :error="firstError(field)"
                required
              >
                <template #default="{ id, invalid, describedBy }">
                  <Textarea
                    :id="id"
                    class="h-auto py-2"
                    :model-value="field.state.value"
                    rows="3"
                    placeholder="What they did"
                    :invalid="invalid"
                    :described-by="describedBy"
                    @update:model-value="field.handleChange($event)"
                    @blur="field.handleBlur"
                  />
                </template>
              </Field>
            </template>
          </suspendForm.Field>

          <suspendForm.Field name="hours">
            <template #default="{ field }">
              <Field label="For how long">
                <template #default="{ id }">
                  <Select
                    :id="id"
                    :model-value="field.state.value"
                    label="For how long"
                    :options="
                      durations.map((choice) => ({ value: choice.hours, label: choice.label }))
                    "
                    @update:model-value="$event !== undefined && field.handleChange($event)"
                  />
                </template>
              </Field>
            </template>
          </suspendForm.Field>

          <div class="flex justify-end gap-2">
            <Button variant="ghost" @click="suspending = null">Cancel</Button>
            <Button variant="danger" type="submit">Suspend</Button>
          </div>
        </form>
      </div>
    </div>
  </div>

  <!--
      Withdrawing an invitation cannot be undone. The link stops working, and
      whoever holds it has no way of asking for another — they do not know who
      to ask.
    -->
  <ConfirmDialog
    v-if="withdrawing"
    title="Withdraw this invitation?"
    detail="The link stops working immediately. Anybody holding it will not be able to join, and will not be told why."
    confirm-label="Withdraw"
    @cancel="withdrawing = null"
    @confirm="confirmWithdrawal"
  />
</template>
