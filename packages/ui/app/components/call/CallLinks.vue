<script setup lang="ts">
import { useForm } from "@tanstack/vue-form";
import type { AnyFieldApi } from "@tanstack/vue-form";
import * as v from "valibot";
import type { CallLink } from "@tdc/types";
import { isApiError } from "@tdc/types";
import { Copy, Link2, Trash2 } from "lucide-vue-next";
import { Button } from "#layers/ui/app/components/ui/button";
import { Field } from "#layers/ui/app/components/ui/field";
import { Input } from "#layers/ui/app/components/ui/input";
import { Switch } from "#layers/ui/app/components/ui/switch";
import { onDayAtTime } from "#layers/ui/app/lib/dates";

/**
 * Links to this conversation's call: making one, copying it, and withdrawing
 * the ones already made.
 *
 * A link lets whoever holds it into the call and nothing else. It can ask for
 * a passcode, hold people in a lobby until somebody here lets them in, expire,
 * and stop after a number of uses. Its address is shown once, when it is made;
 * the server keeps only a hash of it.
 */

const { $t, $ts } = useI18n();
const api = useApi();
const c = useCall();
const serverBase = useMediaBase();

const links = ref<CallLink[]>([]);
const made = ref<string | null>(null);
const copied = ref(false);
const failure = ref<string | null>(null);

const room = computed(() => c.roomId.value);

async function load() {
  if (!room.value) return;
  try {
    links.value = await api.get<CallLink[]>(`/api/rooms/${room.value}/call/links`);
  } catch {
    links.value = [];
  }
}
onMounted(load);

/** The address a link is opened at: this site, or the server in the desktop app. */
function addressOf(secret: string) {
  const base = serverBase || window.location.origin;
  return `${base}/call/${secret}`;
}

/*
 * What a link is made with. A passcode is asked for only when the link is
 * protected, and then must be long enough to be worth asking for.
 */
const linkRules = v.pipe(
  v.object({
    label: v.pipe(v.string(), v.trim(), v.maxLength(80)),
    protect: v.boolean(),
    passcode: v.string(),
    lobby: v.boolean(),
    expiry: v.string(),
    maxUses: v.pipe(v.string(), v.regex(/^\d*$/, $ts("calls.links.usesNumber"))),
  }),
  v.forward(
    v.check((link) => !link.protect || link.passcode.length >= 4, $ts("calls.links.passcodeShort")),
    ["passcode"],
  ),
);

function firstError(field: AnyFieldApi): string | undefined {
  const [issue] = field.state.meta.errors;
  if (!issue) return undefined;
  return typeof issue === "string" ? issue : (issue as { message?: string }).message;
}

const form = useForm({
  defaultValues: {
    label: "",
    protect: false,
    passcode: "",
    lobby: true,
    expiry: "24",
    maxUses: "",
  },
  validators: { onSubmit: linkRules },
  async onSubmit({ value }) {
    if (!room.value) return;
    failure.value = null;
    try {
      const link = await api.post<CallLink>(`/api/rooms/${room.value}/call/links`, {
        label: value.label.trim(),
        passcode: value.protect ? value.passcode : "",
        lobby: value.lobby,
        expiresInHours: Number(value.expiry) || 0,
        maxUses: Number(value.maxUses) || 0,
      });
      made.value = link.secret ? addressOf(link.secret) : null;
      copied.value = false;
      form.reset();
      await load();
    } catch (cause) {
      failure.value = isApiError(cause) ? cause.message : $ts("calls.links.couldNotMake");
    }
  },
});
const protecting = form.useStore((state) => state.values.protect);

async function copy() {
  if (!made.value) return;
  await navigator.clipboard?.writeText(made.value).catch(() => {});
  copied.value = true;
}

async function revoke(link: CallLink) {
  if (!room.value) return;
  const before = links.value;
  links.value = links.value.filter((each) => each.id !== link.id);
  try {
    await api.delete(`/api/rooms/${room.value}/call/links/${link.id}`);
  } catch {
    links.value = before;
  }
}
</script>

<template>
  <div class="max-h-80 space-y-2 overflow-y-auto rounded-lg border bg-muted/30 p-2 text-xs">
    <form class="space-y-2" @submit.prevent="form.handleSubmit()">
      <form.Field name="label">
        <template #default="{ field }">
          <Field :label="$ts('calls.links.label')" :error="firstError(field)">
            <template #default="{ id, invalid, describedBy }">
              <Input
                :id="id"
                :model-value="field.state.value"
                :invalid="invalid"
                :described-by="describedBy"
                @update:model-value="field.handleChange($event)"
                @blur="field.handleBlur"
              />
            </template>
          </Field>
        </template>
      </form.Field>
      <form.Field name="lobby">
        <template #default="{ field }">
          <label class="flex items-center justify-between gap-2">
            <span>{{ $t("calls.links.lobby") }}</span>
            <Switch
              :model-value="field.state.value"
              :label="$ts('calls.links.lobby')"
              @update:model-value="field.handleChange($event)"
            />
          </label>
        </template>
      </form.Field>
      <form.Field name="protect">
        <template #default="{ field }">
          <label class="flex items-center justify-between gap-2">
            <span>{{ $t("calls.links.protect") }}</span>
            <Switch
              :model-value="field.state.value"
              :label="$ts('calls.links.protect')"
              @update:model-value="field.handleChange($event)"
            />
          </label>
        </template>
      </form.Field>
      <form.Field v-if="protecting" name="passcode">
        <template #default="{ field }">
          <Field :label="$ts('calls.links.passcode')" :error="firstError(field)">
            <template #default="{ id, invalid, describedBy }">
              <Input
                :id="id"
                :model-value="field.state.value"
                autocomplete="off"
                :invalid="invalid"
                :described-by="describedBy"
                @update:model-value="field.handleChange($event)"
                @blur="field.handleBlur"
              />
            </template>
          </Field>
        </template>
      </form.Field>
      <div class="flex gap-2">
        <form.Field name="expiry">
          <template #default="{ field }">
            <label class="flex flex-1 flex-col gap-1">
              <span class="text-muted-foreground">{{ $t("calls.links.expires") }}</span>
              <select
                :value="field.state.value"
                class="h-8 rounded-md border bg-background px-1"
                @change="field.handleChange(($event.target as HTMLSelectElement).value)"
              >
                <option value="1">{{ $t("calls.links.hour") }}</option>
                <option value="24">{{ $t("calls.links.day") }}</option>
                <option value="168">{{ $t("calls.links.week") }}</option>
                <option value="0">{{ $t("calls.links.never") }}</option>
              </select>
            </label>
          </template>
        </form.Field>
        <form.Field name="maxUses">
          <template #default="{ field }">
            <Field class="flex-1" :label="$ts('calls.links.uses')" :error="firstError(field)">
              <template #default="{ id, invalid, describedBy }">
                <Input
                  :id="id"
                  :model-value="field.state.value"
                  inputmode="numeric"
                  :placeholder="$ts('calls.links.unlimited')"
                  :invalid="invalid"
                  :described-by="describedBy"
                  @update:model-value="field.handleChange($event)"
                  @blur="field.handleBlur"
                />
              </template>
            </Field>
          </template>
        </form.Field>
      </div>
      <p v-if="failure" class="text-destructive" role="alert">{{ failure }}</p>
      <Button type="submit" size="small" class="w-full" :disabled="form.state.isSubmitting">
        <Link2 class="size-3.5" aria-hidden="true" />
        {{ $t("calls.links.make") }}
      </Button>
    </form>

    <div
      v-if="made"
      class="space-y-1 rounded-md border border-primary/30 bg-background p-2"
      role="status"
    >
      <p class="font-medium">{{ $t("calls.links.madeOnce") }}</p>
      <div class="flex items-center gap-1">
        <code class="min-w-0 flex-1 truncate rounded bg-muted px-1 py-0.5">{{ made }}</code>
        <Button
          variant="ghost"
          size="icon"
          class="size-7"
          :label="$ts('calls.links.copy')"
          @click="copy"
        >
          <Copy class="size-3.5" />
        </Button>
      </div>
      <p v-if="copied" class="text-muted-foreground">{{ $t("calls.links.copied") }}</p>
    </div>

    <ul
      v-if="links.length"
      class="space-y-1 border-t pt-2"
      :aria-label="$ts('calls.links.existing')"
    >
      <li v-for="link in links" :key="link.id" class="flex items-center gap-2">
        <span class="min-w-0 flex-1 truncate">
          <span class="font-medium">{{ link.label || $t("calls.links.untitled") }}</span>
          <span class="text-muted-foreground">
            · {{ $t("calls.links.used", { count: link.uses }) }}
            <template v-if="link.protected"> · {{ $t("calls.links.protected") }}</template>
            <template v-if="link.lobby"> · {{ $t("calls.links.withLobby") }}</template>
            <template v-if="link.expiresAt">
              · {{ $t("calls.links.until", { when: onDayAtTime(link.expiresAt) }) }}</template
            >
          </span>
        </span>
        <Button
          variant="ghost"
          size="icon"
          class="size-6"
          :label="$ts('calls.links.revoke', { name: link.label || $ts('calls.links.untitled') })"
          @click="revoke(link)"
        >
          <Trash2 class="size-3 text-destructive" />
        </Button>
      </li>
    </ul>
  </div>
</template>
