<script setup lang="ts">
import { useForm } from "@tanstack/vue-form";
import type { AnyFieldApi } from "@tanstack/vue-form";
import * as v from "valibot";
import { isApiError } from "@tdc/types";
import type { BackupChannel, BackupPart } from "#layers/ui/app/composables/useOperations";
import { Button } from "#layers/ui/app/components/ui/button";
import { Checkbox } from "#layers/ui/app/components/ui/checkbox";
import { Field } from "#layers/ui/app/components/ui/field";
import { Input } from "#layers/ui/app/components/ui/input";

/**
 * Taking a backup: which parts, optionally which channels, and the passphrase
 * that will be the only way to open it.
 *
 * A part brings what it needs — channels bring accounts, because a message
 * names its sender — and those are shown ticked and fixed, so nobody makes an
 * archive that cannot be restored.
 */
const props = defineProps<{ parts: BackupPart[]; minPassphrase: number }>();
const emit = defineEmits<{ started: [operationId: string] }>();

const { $t, $ts } = useI18n();
const operations = useOperations();

const failure = ref<string | null>(null);
const channels = ref<BackupChannel[]>([]);

function firstError(field: AnyFieldApi): string | undefined {
  const [issue] = field.state.meta.errors;
  if (!issue) return undefined;
  return typeof issue === "string" ? issue : (issue as { message?: string }).message;
}

const rules = v.pipe(
  v.object({
    parts: v.pipe(v.array(v.string()), v.minLength(1, $ts("operations.create.choosePart"))),
    onlySome: v.boolean(),
    rooms: v.array(v.string()),
    passphrase: v.pipe(
      v.string(),
      v.minLength(
        props.minPassphrase,
        $ts("operations.passphraseShort", { length: props.minPassphrase }),
      ),
    ),
    confirm: v.string(),
  }),
  v.forward(
    v.check(
      (value) => value.passphrase === value.confirm,
      $ts("operations.create.passphrasesDiffer"),
    ),
    ["confirm"],
  ),
  v.forward(
    v.check(
      (value) => !value.onlySome || value.rooms.length > 0,
      $ts("operations.create.chooseChannel"),
    ),
    ["rooms"],
  ),
);

const form = useForm({
  defaultValues: {
    parts: props.parts.map((part) => part.name),
    onlySome: false,
    rooms: [] as string[],
    passphrase: "",
    confirm: "",
  },
  validators: { onSubmit: rules },
  async onSubmit({ value }) {
    failure.value = null;
    try {
      const op = await operations.createBackup(
        value.parts,
        value.onlySome ? value.rooms : [],
        value.passphrase,
      );
      form.reset();
      emit("started", op.id);
    } catch (cause) {
      failure.value = isApiError(cause) ? cause.message : $ts("operations.create.failed");
    }
  },
});

const chosen = form.useStore((state) => state.values.parts);
const onlySome = form.useStore((state) => state.values.onlySome);

/** Parts chosen because another chosen part needs them. */
const required = computed(() => {
  const needed = new Set<string>();
  const visit = (name: string) => {
    for (const dependency of props.parts.find((part) => part.name === name)?.needs ?? []) {
      if (!needed.has(dependency)) {
        needed.add(dependency);
        visit(dependency);
      }
    }
  };
  chosen.value.forEach(visit);
  return needed;
});

/** Choosing some channels rules out parts that refer to every one of them. */
const someAllowed = computed(() =>
  chosen.value.every((name) => !props.parts.find((part) => part.name === name)?.needsEveryChannel),
);

function toggle(list: string[], value: string, on: boolean) {
  return on ? [...new Set([...list, value])] : list.filter((item) => item !== value);
}

watch(onlySome, async (on) => {
  if (on && channels.value.length === 0) {
    channels.value = await operations.channels().catch(() => []);
  }
});
</script>

<template>
  <form class="space-y-4" @submit.prevent="form.handleSubmit()">
    <form.Field name="parts">
      <template #default="{ field }">
        <fieldset>
          <legend class="text-sm font-medium">{{ $t("operations.create.whatToKeep") }}</legend>
          <ul class="mt-2 grid gap-1.5 sm:grid-cols-2">
            <li v-for="part in props.parts" :key="part.name" class="flex items-start gap-2">
              <Checkbox
                :id="`part-${part.name}`"
                :model-value="field.state.value.includes(part.name) || required.has(part.name)"
                :disabled="required.has(part.name)"
                @update:model-value="
                  (on: boolean) => field.handleChange(toggle(field.state.value, part.name, on))
                "
              />
              <label :for="`part-${part.name}`" class="text-sm leading-tight">
                {{ $t(`operations.part.${part.name}`) }}
                <span v-if="required.has(part.name)" class="block text-xs text-muted-foreground">
                  {{ $t("operations.create.neededByAnother") }}
                </span>
              </label>
            </li>
          </ul>
          <p v-if="firstError(field)" role="alert" class="mt-1 text-xs text-destructive">
            {{ firstError(field) }}
          </p>
        </fieldset>
      </template>
    </form.Field>

    <form.Field name="onlySome">
      <template #default="{ field }">
        <div class="flex items-start gap-2">
          <Checkbox
            id="backup-only-some"
            :model-value="field.state.value"
            :disabled="!someAllowed"
            @update:model-value="field.handleChange"
          />
          <label for="backup-only-some" class="text-sm leading-tight">
            {{ $t("operations.create.onlySomeChannels") }}
            <span v-if="!someAllowed" class="block text-xs text-muted-foreground">
              {{ $t("operations.create.someNotAllowed") }}
            </span>
          </label>
        </div>
      </template>
    </form.Field>

    <form.Field v-if="onlySome && someAllowed" name="rooms">
      <template #default="{ field }">
        <fieldset class="max-h-48 overflow-y-auto rounded-md border p-2">
          <legend class="px-1 text-xs text-muted-foreground">
            {{ $t("operations.create.whichChannels") }}
          </legend>
          <div v-for="channel in channels" :key="channel.id" class="flex items-center gap-2 py-0.5">
            <Checkbox
              :id="`room-${channel.id}`"
              :model-value="field.state.value.includes(channel.id)"
              @update:model-value="
                (on: boolean) => field.handleChange(toggle(field.state.value, channel.id, on))
              "
            />
            <label :for="`room-${channel.id}`" class="text-sm">{{ channel.name }}</label>
          </div>
          <p v-if="firstError(field)" role="alert" class="mt-1 text-xs text-destructive">
            {{ firstError(field) }}
          </p>
        </fieldset>
      </template>
    </form.Field>

    <div class="grid gap-3 sm:grid-cols-2">
      <form.Field name="passphrase">
        <template #default="{ field }">
          <Field
            :label="$ts('operations.passphrase')"
            :hint="$ts('operations.create.passphraseHint')"
            :error="firstError(field)"
          >
            <template #default="{ id, invalid, describedBy }">
              <Input
                :id="id"
                type="password"
                autocomplete="new-password"
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
      <form.Field name="confirm">
        <template #default="{ field }">
          <Field :label="$ts('operations.create.confirmPassphrase')" :error="firstError(field)">
            <template #default="{ id, invalid, describedBy }">
              <Input
                :id="id"
                type="password"
                autocomplete="new-password"
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
    </div>

    <p
      v-if="failure"
      role="alert"
      class="rounded-md bg-destructive/10 px-3 py-2 text-sm text-destructive"
    >
      {{ failure }}
    </p>
    <Button type="submit">{{ $t("operations.create.start") }}</Button>
  </form>
</template>
