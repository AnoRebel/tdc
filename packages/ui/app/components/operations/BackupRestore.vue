<script setup lang="ts">
import { useForm } from "@tanstack/vue-form";
import type { AnyFieldApi } from "@tanstack/vue-form";
import * as v from "valibot";
import { isApiError } from "@tdc/types";
import { Undo2 } from "lucide-vue-next";
import type {
  Backup,
  BackupPart,
  Operation,
  RestoreResult,
} from "#layers/ui/app/composables/useOperations";
import { Button } from "#layers/ui/app/components/ui/button";
import { Checkbox } from "#layers/ui/app/components/ui/checkbox";
import { Field } from "#layers/ui/app/components/ui/field";
import { Input } from "#layers/ui/app/components/ui/input";

/**
 * Restoring from one archive: everything, or some of it, previewed first.
 *
 * A preview of a partial restore is the restore itself, rolled back, so what
 * it reports is what would happen. The restore button is offered once a
 * preview has run with the same choices, and asks again before it acts.
 */
const props = defineProps<{ backup: Backup; parts: BackupPart[]; minPassphrase: number }>();
const emit = defineEmits<{ started: [operationId: string] }>();

const { $t, $ts } = useI18n();
const operations = useOperations();

const failure = ref<string | null>(null);
const previewing = ref<Operation | null>(null);
const preview = ref<RestoreResult | null>(null);
const previewedWith = ref("");
const confirming = ref(false);
let intent: "preview" | "restore" = "preview";

/** Parts that can be restored on their own, alongside what is already here. */
const mergeable = ["settings", "channels", "files"];
const inArchive = computed(() => props.backup.manifest.parts ?? []);
const notPartial = computed(() =>
  props.parts.filter((part) => part.notPartial && inArchive.value.includes(part.name)),
);
const complete = computed(
  () =>
    !props.backup.manifest.rooms?.length &&
    props.parts.every((part) => inArchive.value.includes(part.name)),
);

function firstError(field: AnyFieldApi): string | undefined {
  const [issue] = field.state.meta.errors;
  if (!issue) return undefined;
  return typeof issue === "string" ? issue : (issue as { message?: string }).message;
}

const rules = v.pipe(
  v.object({
    mode: v.picklist(["replace", "merge"]),
    parts: v.array(v.string()),
    passphrase: v.pipe(v.string(), v.minLength(1, $ts("operations.restore.passphraseNeeded"))),
  }),
  v.forward(
    v.check(
      (value) => value.mode === "replace" || value.parts.length > 0,
      $ts("operations.create.choosePart"),
    ),
    ["parts"],
  ),
);

const form = useForm({
  defaultValues: {
    mode: (complete.value ? "replace" : "merge") as "replace" | "merge",
    parts: [] as string[],
    passphrase: "",
  },
  validators: { onSubmit: rules },
  async onSubmit({ value }) {
    failure.value = null;
    const key = JSON.stringify([value.mode, [...value.parts].sort()]);
    if (intent === "restore" && previewedWith.value !== key) {
      failure.value = $ts("operations.restore.previewFirst");
      return;
    }
    if (intent === "restore" && !confirming.value) {
      confirming.value = true;
      return;
    }
    confirming.value = false;
    const parts = value.mode === "replace" ? [] : value.parts;
    try {
      const op = await operations.restore(props.backup.id, {
        mode: value.mode,
        parts,
        rooms: [],
        passphrase: value.passphrase,
        dryRun: intent === "preview",
      });
      if (intent === "restore") {
        emit("started", op.id);
        return;
      }
      preview.value = null;
      const ended = await operations.follow(op.id, (reading) => (previewing.value = reading));
      previewing.value = null;
      if (ended?.status === "succeeded") {
        preview.value = ended.result as unknown as RestoreResult;
        previewedWith.value = key;
      } else {
        failure.value = ended?.error ?? $ts("operations.restore.previewFailed");
      }
    } catch (cause) {
      failure.value = isApiError(cause) ? cause.message : $ts("operations.restore.failed");
    }
  },
});

const mode = form.useStore((state) => state.values.mode);

function submit(which: "preview" | "restore") {
  intent = which;
  void form.handleSubmit();
}

function toggle(list: string[], value: string, on: boolean) {
  const next = on ? [...new Set([...list, value])] : list.filter((item) => item !== value);
  // Files come back with the channels they belong to.
  return next.includes("files") && !next.includes("channels") ? [...next, "channels"] : next;
}

const changed = computed(() =>
  (preview.value?.tables ?? []).filter((table) => table.inArchive > 0),
);
</script>

<template>
  <form class="space-y-4" @submit.prevent="submit('preview')">
    <form.Field name="mode">
      <template #default="{ field }">
        <fieldset class="space-y-2">
          <legend class="text-sm font-medium">{{ $t("operations.restore.how") }}</legend>
          <label class="flex items-start gap-2 text-sm" :class="{ 'opacity-50': !complete }">
            <input
              type="radio"
              name="restore-mode"
              value="replace"
              class="mt-0.5 accent-primary"
              :checked="field.state.value === 'replace'"
              :disabled="!complete"
              @change="field.handleChange('replace')"
            />
            <span>
              {{ $t("operations.restore.replace") }}
              <span class="block text-xs text-muted-foreground">
                {{
                  complete
                    ? $t("operations.restore.replaceHint")
                    : $t("operations.restore.replaceNeedsEverything")
                }}
              </span>
            </span>
          </label>
          <label class="flex items-start gap-2 text-sm">
            <input
              type="radio"
              name="restore-mode"
              value="merge"
              class="mt-0.5 accent-primary"
              :checked="field.state.value === 'merge'"
              @change="field.handleChange('merge')"
            />
            <span>
              {{ $t("operations.restore.merge") }}
              <span class="block text-xs text-muted-foreground">{{
                $t("operations.restore.mergeHint")
              }}</span>
            </span>
          </label>
        </fieldset>
      </template>
    </form.Field>

    <form.Field v-if="mode === 'merge'" name="parts">
      <template #default="{ field }">
        <fieldset>
          <legend class="text-sm font-medium">{{ $t("operations.restore.whatToRestore") }}</legend>
          <div
            v-for="name in mergeable.filter((part) => inArchive.includes(part))"
            :key="name"
            class="mt-1.5 flex items-center gap-2"
          >
            <Checkbox
              :id="`restore-${name}`"
              :model-value="field.state.value.includes(name)"
              @update:model-value="
                (on: boolean) => field.handleChange(toggle(field.state.value, name, on))
              "
            />
            <label :for="`restore-${name}`" class="text-sm">{{
              $t(`operations.part.${name}`)
            }}</label>
          </div>
          <p v-if="firstError(field)" role="alert" class="mt-1 text-xs text-destructive">
            {{ firstError(field) }}
          </p>
          <details v-if="notPartial.length" class="mt-2 text-xs text-muted-foreground">
            <summary class="cursor-pointer">{{ $t("operations.restore.notPartialTitle") }}</summary>
            <ul class="mt-1 list-disc space-y-1 pl-5">
              <li v-for="part in notPartial" :key="part.name">
                <strong>{{ $t(`operations.part.${part.name}`) }}</strong> —
                {{ $t(`operations.notPartial.${part.name}`) }}
              </li>
            </ul>
          </details>
        </fieldset>
      </template>
    </form.Field>

    <form.Field name="passphrase">
      <template #default="{ field }">
        <Field :label="$ts('operations.passphrase')" :error="firstError(field)">
          <template #default="{ id, invalid, describedBy }">
            <Input
              :id="id"
              type="password"
              autocomplete="off"
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

    <OperationsProgress v-if="previewing" :operation="previewing" />

    <div v-if="preview" class="rounded-lg border bg-surface p-3 text-sm">
      <p class="font-medium">{{ $t("operations.restore.previewTitle") }}</p>
      <p v-if="preview.restartRequired" class="mt-1 text-xs text-warning">
        {{ $t("operations.restore.restartWarning") }}
      </p>
      <div class="mt-2 max-h-64 overflow-auto">
        <table class="w-full text-xs">
          <thead>
            <tr class="text-left text-muted-foreground">
              <th scope="col" class="py-1 pr-2">{{ $t("operations.restore.table") }}</th>
              <th scope="col" class="py-1 pr-2 text-right">
                {{ $t("operations.restore.inArchive") }}
              </th>
              <th scope="col" class="py-1 pr-2 text-right">
                {{
                  preview.mode === "replace"
                    ? $t("operations.restore.now")
                    : $t("operations.restore.added")
                }}
              </th>
              <th v-if="preview.mode === 'merge'" scope="col" class="py-1 text-right">
                {{ $t("operations.restore.skipped") }}
              </th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="table in changed" :key="table.table" class="border-t">
              <td class="py-1 pr-2 font-mono">{{ table.table }}</td>
              <td class="tabular py-1 pr-2 text-right">{{ table.inArchive }}</td>
              <td class="tabular py-1 pr-2 text-right">
                {{ preview.mode === "replace" ? table.current : table.added }}
              </td>
              <td v-if="preview.mode === 'merge'" class="tabular py-1 text-right">
                {{ table.skipped }}
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <ul v-if="preview.settings?.length" class="mt-2 space-y-0.5 text-xs">
        <li v-for="setting in preview.settings" :key="setting.key">
          <span class="font-mono">{{ setting.key }}</span
          >: {{ setting.current || "—" }} → {{ setting.archive }}
        </li>
      </ul>
      <p class="mt-2 text-xs text-muted-foreground">
        {{
          $t("operations.restore.files", {
            written: preview.filesWritten,
            present: preview.filesPresent,
          })
        }}
      </p>
    </div>

    <p
      v-if="failure"
      role="alert"
      class="rounded-md bg-destructive/10 px-3 py-2 text-sm text-destructive"
    >
      {{ failure }}
    </p>

    <div class="flex flex-wrap gap-2">
      <Button type="submit" variant="outline">{{ $t("operations.restore.preview") }}</Button>
      <Button variant="danger" :disabled="!preview" @click="submit('restore')">
        <Undo2 class="size-3.5" />
        {{ $t("operations.restore.apply") }}
      </Button>
    </div>
  </form>

  <ConfirmDialog
    v-if="confirming"
    :title="$ts('operations.restore.confirmTitle')"
    :detail="
      mode === 'replace'
        ? $ts('operations.restore.confirmReplace')
        : $ts('operations.restore.confirmMerge')
    "
    :confirm-label="$ts('operations.restore.apply')"
    :cancel-label="$ts('operations.cancel')"
    @cancel="confirming = false"
    @confirm="submit('restore')"
  />
</template>
