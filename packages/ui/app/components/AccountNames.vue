<script setup lang="ts">
import { useForm } from "@tanstack/vue-form";
import type { AnyFieldApi } from "@tanstack/vue-form";
import { watchDebounced } from "@vueuse/core";
import * as v from "valibot";
import { AtSign, CircleAlert, CircleCheck, IdCard } from "lucide-vue-next";

import { isApiError } from "@tdc/types";
import type { UsernameAvailability, UsernamePolicy } from "@tdc/types";
import { Button } from "#layers/ui/app/components/ui/button";
import { Field } from "#layers/ui/app/components/ui/field";
import { Input } from "#layers/ui/app/components/ui/input";
import { Spinner } from "#layers/ui/app/components/ui/spinner";
import { onDate } from "#layers/ui/app/lib/dates";
import { displayNameProblem, usernameProblem } from "#layers/ui/app/lib/names";

const { $t, $ts } = useI18n();

/**
 * What somebody is called: the display name people read, and the username that
 * is half of their address.
 *
 * Side by side because people think of them as one thing — "my name" — and
 * apart in weight, because they are not. The display name saves like any
 * preference. The username asks first: it says what changes, what does not,
 * and for the password, because a name taken by whoever found a laptop open is
 * the impersonation this exists to prevent.
 */

const { user } = useAuth();
const names = useAccountNames();

function firstError(field: AnyFieldApi): string | undefined {
  if (!field.state.meta.isTouched) return undefined;
  const [issue] = field.state.meta.errors;
  if (!issue) return undefined;
  return typeof issue === "string" ? issue : (issue as { message?: string }).message;
}

/* The display name. */

const displayNameSaved = ref(false);
const displayNameError = ref<string | null>(null);

const displayNameSchema = v.object({
  displayName: v.pipe(
    v.string(),
    v.check((value) => displayNameProblem(value) !== "length", $ts("names.displayNameTooLong")),
    v.check(
      (value) => displayNameProblem(value) !== "characters",
      $ts("names.displayNameCharacters"),
    ),
  ),
});

const displayNameForm = useForm({
  defaultValues: { displayName: user.value?.displayName ?? "" },
  validators: { onChange: displayNameSchema, onSubmit: displayNameSchema },
  async onSubmit({ value }) {
    displayNameError.value = null;
    displayNameSaved.value = false;
    try {
      const updated = await names.setDisplayName(value.displayName.trim());
      displayNameForm.reset({ displayName: updated.displayName });
      displayNameSaved.value = true;
    } catch (cause) {
      displayNameError.value = isApiError(cause) ? cause.message : $ts("names.couldNotSave");
    }
  },
});

const displayNameDraft = displayNameForm.useSelector((state) => state.values.displayName);
const savingDisplayName = displayNameForm.useSelector((state) => state.isSubmitting);
const displayNameChanged = computed(
  () => displayNameDraft.value.trim() !== (user.value?.displayName ?? ""),
);

/* The username. */

const policy = ref<UsernamePolicy | null>(null);
const policyError = ref<string | null>(null);

async function loadPolicy() {
  try {
    policy.value = await names.usernamePolicy();
  } catch (cause) {
    policyError.value = isApiError(cause) ? cause.message : $ts("names.couldNotRead");
  }
}

onMounted(loadPolicy);

/*
 * Why somebody may not change it, in their language rather than the server's.
 * The server's own message is the fallback for a reason this client does not
 * know yet.
 */
const refusal = computed(() => {
  const current = policy.value;
  if (!current || current.canChange) return null;
  switch (current.refusal) {
    case "disabled":
      return $ts("names.refusedDisabled");
    case "federated":
      return $ts("names.refusedFederated");
    case "waiting":
      return current.nextChangeAt
        ? $ts("names.refusedWaiting", { date: onDate(current.nextChangeAt) })
        : (current.message ?? "");
    case "token":
      return $ts("names.refusedToken");
    default:
      return current.message ?? "";
  }
});

const wanted = ref("");
const checking = ref(false);
const answer = ref<UsernameAvailability | null>(null);
const confirming = ref(false);
const changed = ref<string | null>(null);

/** What is wrong with the shape of what was typed, said before asking anybody. */
const shapeProblem = computed(() => {
  const value = wanted.value.trim();
  if (!value) return null;
  switch (usernameProblem(value)) {
    case "length":
      return $ts("names.usernameLength");
    case "characters":
      return $ts("names.usernameCharacters");
    case "edges":
      return $ts("names.usernameEdges");
    default:
      return null;
  }
});

/*
 * Asked as somebody types, once they pause. A name that has the wrong shape is
 * not sent at all: the field already says what is wrong, and the server would
 * only say it again.
 */
watchDebounced(
  wanted,
  async (value) => {
    const name = value.trim();
    answer.value = null;
    confirming.value = false;
    if (!name || shapeProblem.value || !policy.value?.canChange) return;

    checking.value = true;
    try {
      const result = await names.availability(name);
      // Only if it is still what is in the box: a slow answer for an earlier
      // spelling must not mark a later one as free.
      if (wanted.value.trim() === name) answer.value = result;
    } catch (cause) {
      answer.value = {
        username: name,
        available: false,
        message: isApiError(cause) ? cause.message : $ts("names.couldNotCheck"),
      };
    } finally {
      checking.value = false;
    }
  },
  { debounce: 400 },
);

const availabilityMessage = computed(() => {
  const result = answer.value;
  if (!result || result.available) return null;
  switch (result.reason) {
    case "current":
      return $ts("names.alreadyYours");
    case "taken":
      return $ts("names.taken");
    case "held":
      return $ts("names.held");
    default:
      return result.message ?? $ts("names.notAvailable");
  }
});

const usernameError = computed(() => shapeProblem.value ?? availabilityMessage.value ?? undefined);
const available = computed(
  () => answer.value?.available === true && answer.value.username === wanted.value.trim(),
);

const newAddress = computed(() => {
  const address = user.value?.address ?? "";
  const server = address.slice(address.indexOf("@") + 1);
  return `${wanted.value.trim()}@${server}`;
});

/* The confirmation, which asks for the password where there is one. */

const confirmError = ref<string | null>(null);
const confirmHeading = ref<HTMLElement | null>(null);

const needsPassword = computed(() => policy.value?.confirm === "password");
const signInStale = computed(() => {
  const until = policy.value?.recentSignInUntil;
  return !needsPassword.value && (!until || new Date(until).getTime() < Date.now());
});

const confirmSchema = v.object({
  password: v.string(),
});

const confirmForm = useForm({
  defaultValues: { password: "" },
  validators: {
    onSubmit: v.pipe(
      confirmSchema,
      v.check(
        (value) => !needsPassword.value || value.password.length > 0,
        $ts("names.passwordNeeded"),
      ),
    ),
  },
  async onSubmit({ value }) {
    confirmError.value = null;
    const name = wanted.value.trim();
    try {
      await names.changeUsername(name, value.password);
      changed.value = name;
      confirming.value = false;
      wanted.value = "";
      answer.value = null;
      confirmForm.reset();
      await loadPolicy();
    } catch (cause) {
      confirmError.value = isApiError(cause) ? cause.message : $ts("names.couldNotChange");
    }
  },
});

const changingUsername = confirmForm.useSelector((state) => state.isSubmitting);

async function beginConfirming() {
  changed.value = null;
  confirmError.value = null;
  confirmForm.reset();
  confirming.value = true;
  // Focus follows the step, so somebody on a keyboard or a screen reader is
  // taken to the question rather than left on a button that has gone quiet.
  await nextTick();
  confirmHeading.value?.focus();
}
</script>

<template>
  <section class="space-y-5" aria-labelledby="your-name">
    <div>
      <h2 id="your-name" class="flex items-center gap-1.5 text-sm font-semibold">
        <IdCard class="size-4" />
        {{ $t("names.title") }}
      </h2>
      <p class="mt-1 text-xs text-muted-foreground">{{ $t("names.hint") }}</p>
    </div>

    <!-- The display name. -->
    <form class="flex items-start gap-2" @submit.prevent="displayNameForm.handleSubmit()">
      <div class="flex-1">
        <displayNameForm.Field name="displayName">
          <template #default="{ field }">
            <Field
              :label="$ts('names.displayName')"
              :hint="$ts('names.displayNameHint')"
              :error="firstError(field) ?? displayNameError ?? undefined"
            >
              <template #default="{ id, invalid, describedBy }">
                <Input
                  :id="id"
                  :model-value="field.state.value"
                  autocomplete="name"
                  :placeholder="user?.username"
                  :invalid="invalid"
                  :described-by="describedBy"
                  @update:model-value="
                    field.handleChange($event);
                    displayNameSaved = false;
                  "
                  @blur="field.handleBlur"
                />
              </template>
            </Field>
          </template>
        </displayNameForm.Field>
      </div>
      <Button
        type="submit"
        variant="outline"
        class="mt-5 h-9"
        :disabled="!displayNameChanged || savingDisplayName"
      >
        <Spinner v-if="savingDisplayName" size="sm" />
        {{ $t("names.save") }}
      </Button>
    </form>
    <p
      v-if="displayNameSaved"
      class="-mt-3 flex items-center gap-1.5 text-xs text-success-text"
      role="status"
    >
      <CircleCheck class="size-3.5" aria-hidden="true" />
      {{ $t("names.displayNameSaved") }}
    </p>

    <!-- The username. -->
    <div class="space-y-3 rounded-lg border bg-surface p-3">
      <div class="flex items-start gap-2">
        <AtSign class="mt-0.5 size-4 shrink-0 text-muted-foreground" aria-hidden="true" />
        <div class="min-w-0">
          <p class="text-sm font-medium">{{ $t("names.username") }}</p>
          <p class="text-xs text-muted-foreground">
            {{ $t("names.yourAddressIs") }}
            <span class="font-mono text-foreground">{{ user?.address }}</span>
          </p>
        </div>
      </div>

      <p v-if="changed" class="rounded-md bg-primary/10 px-3 py-2 text-sm" role="status">
        {{ $t("names.changedTo", { username: changed }) }}
      </p>

      <p
        v-if="policyError"
        class="rounded-md bg-destructive/10 px-3 py-2 text-sm text-destructive"
        role="alert"
      >
        {{ policyError }}
      </p>

      <p v-else-if="!policy" class="flex items-center gap-2 text-sm text-muted-foreground">
        <Spinner size="sm" />
        {{ $t("names.reading") }}
      </p>

      <!-- Why not, before anybody types: a field that refuses every name
           without saying why is a field people give up on. -->
      <p
        v-else-if="refusal"
        class="flex gap-2 rounded-md bg-muted px-3 py-2 text-sm"
        data-username-refusal
      >
        <CircleAlert class="mt-0.5 size-4 shrink-0 text-muted-foreground" aria-hidden="true" />
        <span>{{ refusal }}</span>
      </p>

      <template v-else>
        <Field
          :label="$ts('names.newUsername')"
          :hint="$ts('names.usernameHint')"
          :error="usernameError"
        >
          <template #default="{ id, invalid, describedBy }">
            <div class="relative">
              <Input
                :id="id"
                v-model="wanted"
                autocomplete="off"
                autocapitalize="off"
                spellcheck="false"
                :placeholder="user?.username"
                :invalid="invalid"
                :described-by="describedBy"
                class="pr-8"
                :disabled="confirming"
              />
              <span
                class="pointer-events-none absolute inset-y-0 right-2.5 grid place-items-center"
              >
                <Spinner v-if="checking" size="sm" />
                <CircleCheck
                  v-else-if="available"
                  class="size-4 text-success-text"
                  aria-hidden="true"
                />
              </span>
            </div>
          </template>
        </Field>
        <!-- Said, not only drawn: the tick means nothing to somebody who
             cannot see it. -->
        <p v-if="available" class="-mt-2 text-xs text-success-text" role="status">
          {{ $t("names.available", { address: newAddress }) }}
        </p>

        <Button
          v-if="!confirming"
          variant="outline"
          :disabled="!available"
          @click="beginConfirming"
        >
          {{ $t("names.changeUsername") }}
        </Button>

        <!-- The second step: what changes, what does not, and proof. -->
        <form
          v-else
          class="space-y-3 rounded-md border border-border-strong p-3"
          aria-labelledby="confirm-username"
          @submit.prevent="confirmForm.handleSubmit()"
        >
          <h3
            id="confirm-username"
            ref="confirmHeading"
            tabindex="-1"
            class="text-sm font-semibold outline-none"
          >
            {{ $t("names.confirmTitle", { username: wanted.trim() }) }}
          </h3>
          <ul class="list-disc space-y-1 pl-4 text-xs text-muted-foreground">
            <li>{{ $t("names.consequenceAddress", { address: newAddress }) }}</li>
            <li v-if="(policy?.holdDays ?? 0) > 0">
              {{
                $t("names.consequenceHeld", {
                  username: user?.username ?? "",
                  days: String(policy?.holdDays ?? 0),
                })
              }}
            </li>
            <li v-else>
              {{ $t("names.consequenceReleased", { username: user?.username ?? "" }) }}
            </li>
            <li>{{ $t("names.consequenceMessages") }}</li>
            <li>{{ $t("names.consequenceElsewhere") }}</li>
            <li v-if="(policy?.intervalDays ?? 0) > 0">
              {{ $t("names.consequenceWait", { days: String(policy?.intervalDays ?? 0) }) }}
            </li>
          </ul>

          <confirmForm.Field v-if="needsPassword" name="password">
            <template #default="{ field }">
              <Field
                :label="$ts('names.password')"
                :error="firstError(field) ?? confirmError ?? undefined"
              >
                <template #default="{ id, invalid, describedBy }">
                  <Input
                    :id="id"
                    type="password"
                    :model-value="field.state.value"
                    autocomplete="current-password"
                    :invalid="invalid"
                    :described-by="describedBy"
                    @update:model-value="field.handleChange($event)"
                    @blur="field.handleBlur"
                  />
                </template>
              </Field>
            </template>
          </confirmForm.Field>
          <template v-else>
            <p v-if="signInStale" class="rounded-md bg-muted px-3 py-2 text-xs">
              {{ $t("names.signInAgain") }}
            </p>
            <p v-if="confirmError" class="text-xs text-destructive" role="alert">
              {{ confirmError }}
            </p>
          </template>

          <div class="flex flex-wrap gap-2">
            <Button type="submit" :disabled="changingUsername || signInStale">
              <Spinner v-if="changingUsername" size="sm" />
              {{ $t("names.confirmChange") }}
            </Button>
            <Button variant="ghost" @click="confirming = false">{{ $t("names.cancel") }}</Button>
          </div>
        </form>
      </template>

      <p v-if="policy?.formerUsernames.length" class="text-xs text-muted-foreground">
        {{
          $t("names.formerlyYou", {
            names: policy.formerUsernames
              .map((former) => `${former.username} (${onDate(former.changedAt)})`)
              .join(", "),
          })
        }}
      </p>
    </div>
  </section>
</template>
