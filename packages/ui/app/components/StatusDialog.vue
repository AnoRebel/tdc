<script setup lang="ts">
import { isApiError } from "@tdc/types";
import { useForm } from "@tanstack/vue-form";
import { onKeyStroke } from "@vueuse/core";
import * as v from "valibot";

import { Button } from "#layers/ui/app/components/ui/button";
import { Field } from "#layers/ui/app/components/ui/field";
import { Input } from "#layers/ui/app/components/ui/input";
import { Select } from "#layers/ui/app/components/ui/select";
import { atTime, onDayAtTime } from "#layers/ui/app/lib/dates";
import { presenceDotClass } from "#layers/ui/app/lib/presencedot";
import { appearOfflineKey } from "#layers/ui/app/composables/useOwnStatus";
import {
  expiryChoices,
  lapsesToday,
  secondsUntil,
  secondsUntilMoment,
  type ExpiryChoice,
} from "#layers/ui/app/lib/statusexpiry";

/**
 * Setting a status: which one, a few words with it, and when it clears.
 *
 * "Clear after" is what makes a status worth setting. "In a meeting" left on
 * overnight is worse than no status, because the next morning it is a lie, and
 * nobody remembers to take it off.
 */

const open = defineModel<boolean>({ default: false });

const { $t, $ts } = useI18n();
const status = useOwnStatus();
const { say } = useNotice();
const error = ref<string | null>(null);

type Choice = ExpiryChoice | "custom";

const clearOptions = computed(() => [
  ...expiryChoices.map((choice) => ({
    value: choice as Choice,
    label: $ts(`status.clear.${choice}`),
  })),
  { value: "custom" as Choice, label: $ts("status.clear.custom") },
]);

const schema = computed(() =>
  v.pipe(
    v.object({
      status: v.pipe(v.string(), v.minLength(1)),
      text: v.pipe(v.string(), v.maxLength(100, $ts("status.noteTooLong"))),
      clearAfter: v.picklist([...expiryChoices, "custom"] as Choice[]),
      at: v.nullable(v.date()),
    }),
    v.forward(
      v.check(
        (value) => value.clearAfter !== "custom" || (!!value.at && !!secondsUntilMoment(value.at)),
        $ts("status.pickAFutureTime"),
      ),
      ["at"],
    ),
  ),
);

const form = useForm({
  defaultValues: {
    status: "available",
    text: "",
    clearAfter: "never" as Choice,
    at: null as Date | null,
  },
  validators: { onSubmit: schema.value },
  async onSubmit({ value }) {
    error.value = null;
    const expiresInSeconds =
      value.clearAfter === "custom"
        ? value.at
          ? secondsUntilMoment(value.at)
          : undefined
        : secondsUntil(value.clearAfter as ExpiryChoice);
    try {
      await status.set({ status: value.status, text: value.text.trim(), expiresInSeconds });
      open.value = false;
      say($ts("status.saved"));
    } catch (cause) {
      error.value = isApiError(cause) ? cause.message : $ts("status.couldNotSave");
    }
  },
});

/*
 * Opened on what is set now, so changing the note does not quietly reset the
 * status, and keeping the status does not quietly drop the note.
 */
watch(
  open,
  (isOpen) => {
    if (!isOpen) return;
    error.value = null;
    const own = status.own.value;
    form.reset({
      status: own.status,
      text: own.statusText,
      clearAfter: own.expiresAt ? "custom" : "never",
      at: own.expiresAt ? new Date(own.expiresAt) : null,
    });
  },
  { immediate: true },
);

async function clearStatus() {
  error.value = null;
  try {
    await status.clear();
    open.value = false;
    say($ts("status.cleared"));
  } catch {
    error.value = $ts("status.couldNotSave");
  }
}

/** When a chosen preset ends, said beside it so nobody has to work it out. */
function endsAt(choice: Choice): string | undefined {
  if (choice === "custom" || choice === "never") return undefined;
  const seconds = secondsUntil(choice);
  if (!seconds) return undefined;
  const when = new Date(Date.now() + seconds * 1000);
  return lapsesToday(when) ? atTime(when) : onDayAtTime(when);
}

const panel = useTemplateRef<HTMLElement>("panel");
let returnTo: HTMLElement | null = null;
watch(panel, (element) => {
  if (element) {
    returnTo = document.activeElement as HTMLElement | null;
    element.querySelector<HTMLElement>("input[type=radio]:checked, input")?.focus();
  } else {
    returnTo?.focus?.();
    returnTo = null;
  }
});

onKeyStroke("Escape", (event) => {
  if (!open.value) return;
  event.preventDefault();
  open.value = false;
});

const isDark = ref(true);
onMounted(() => {
  isDark.value = !document.documentElement.classList.contains("light");
});
const earliest = computed(() => new Date(Date.now() + 60_000));
</script>

<template>
  <Teleport to="body">
    <Transition name="motion-fade">
      <!-- eslint-disable-next-line vuejs-accessibility/no-static-element-interactions -->
      <div
        v-if="open"
        class="fixed inset-0 z-50 flex items-center justify-center bg-background/70 p-4 backdrop-blur-sm"
        @click.self="open = false"
      >
        <Transition name="motion-pop" appear>
          <form
            ref="panel"
            class="flex max-h-[calc(100dvh-2rem)] w-[min(26rem,100%)] flex-col overflow-hidden rounded-xl border border-border bg-card shadow-2xl"
            role="dialog"
            aria-modal="true"
            :aria-label="$ts('status.title')"
            @submit.prevent="form.handleSubmit()"
          >
            <h2 class="border-b px-4 py-3 text-sm font-semibold">{{ $t("status.title") }}</h2>

            <div class="min-h-0 flex-1 space-y-4 overflow-y-auto p-4">
              <form.Field name="status">
                <template #default="{ field }">
                  <fieldset>
                    <legend class="mb-1.5 text-xs font-medium">{{ $t("status.which") }}</legend>
                    <div class="grid grid-cols-2 gap-1.5">
                      <label
                        v-for="option in status.statuses.value"
                        :key="option.key"
                        class="flex cursor-pointer items-center gap-2 rounded-lg border px-2.5 py-2 text-sm transition-colors has-[:checked]:border-primary has-[:checked]:bg-primary/8 has-[:focus-visible]:ring-2 has-[:focus-visible]:ring-ring/40"
                      >
                        <input
                          type="radio"
                          name="status"
                          class="sr-only"
                          :value="option.key"
                          :checked="field.state.value === option.key"
                          @change="field.handleChange(option.key)"
                        />
                        <span
                          class="size-2.5 shrink-0 rounded-full"
                          :class="presenceDotClass(option.key, option.key === appearOfflineKey)"
                          aria-hidden="true"
                        />
                        <span class="min-w-0">
                          <span class="block truncate">{{ option.label }}</span>
                          <span
                            v-if="option.suppressesNotifications"
                            class="block truncate text-[0.6875rem] text-muted-foreground"
                          >
                            {{ $t("status.holdsNotifications") }}
                          </span>
                        </span>
                      </label>
                    </div>
                    <p
                      v-if="field.state.value === appearOfflineKey"
                      class="mt-1.5 text-xs text-muted-foreground"
                    >
                      {{ $t("status.appearOfflineHint") }}
                    </p>
                  </fieldset>
                </template>
              </form.Field>

              <form.Field name="text">
                <template #default="{ field }">
                  <Field
                    :label="$ts('status.note')"
                    :hint="$ts('status.noteHint')"
                    :error="
                      field.state.meta.errors[0]
                        ? String(
                            (field.state.meta.errors[0] as { message?: string }).message ??
                              field.state.meta.errors[0],
                          )
                        : undefined
                    "
                  >
                    <template #default="{ id, invalid, describedBy }">
                      <Input
                        :id="id"
                        :model-value="field.state.value"
                        :placeholder="$ts('status.notePlaceholder')"
                        autocomplete="off"
                        :invalid="invalid"
                        :described-by="describedBy"
                        @update:model-value="field.handleChange($event)"
                      />
                    </template>
                  </Field>
                </template>
              </form.Field>

              <form.Field name="clearAfter">
                <template #default="{ field }">
                  <Field :label="$ts('status.clearAfter')">
                    <template #default="{ id }">
                      <Select
                        :id="id"
                        :model-value="field.state.value"
                        :label="$ts('status.clearAfter')"
                        :options="
                          clearOptions.map((option) => ({
                            ...option,
                            label: endsAt(option.value)
                              ? `${option.label} · ${endsAt(option.value)}`
                              : option.label,
                          }))
                        "
                        @update:model-value="$event !== undefined && field.handleChange($event)"
                      />
                    </template>
                  </Field>

                  <form.Field v-if="field.state.value === 'custom'" name="at">
                    <template #default="{ field: at }">
                      <div class="mt-2">
                        <ClientOnly>
                          <VueDatePicker
                            :model-value="at.state.value"
                            :min-date="earliest"
                            :dark="isDark"
                            inline
                            auto-apply
                            time-picker-inline
                            :clearable="false"
                            :action-row="{
                              showSelect: false,
                              showCancel: false,
                              showNow: false,
                              showPreview: false,
                            }"
                            @update:model-value="at.handleChange($event as Date)"
                          />
                          <template #fallback>
                            <p class="text-xs text-muted-foreground">
                              {{ $t("room.loadingCalendar") }}
                            </p>
                          </template>
                        </ClientOnly>
                        <p
                          v-if="at.state.meta.errors[0]"
                          role="alert"
                          class="mt-1 text-xs text-destructive"
                        >
                          {{ $t("status.pickAFutureTime") }}
                        </p>
                      </div>
                    </template>
                  </form.Field>
                </template>
              </form.Field>

              <p v-if="error" role="alert" class="text-xs text-destructive">{{ error }}</p>
            </div>

            <footer class="flex items-center justify-between gap-2 border-t px-4 py-3">
              <Button
                v-if="status.own.value.status !== 'available' || status.own.value.statusText"
                variant="ghost"
                @click="clearStatus"
              >
                {{ $t("status.clearNow") }}
              </Button>
              <span v-else />
              <span class="flex gap-2">
                <Button variant="ghost" @click="open = false">{{ $t("action.cancel") }}</Button>
                <Button type="submit">{{ $t("action.save") }}</Button>
              </span>
            </footer>
          </form>
        </Transition>
      </div>
    </Transition>
  </Teleport>
</template>
