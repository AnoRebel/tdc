<script setup lang="ts">
import { isApiError } from "@tdc/types";
import { ArrowLeft, Download, ShieldAlert, Trash2, TriangleAlert, Upload } from "lucide-vue-next";
import type { Backup, Operation } from "#layers/ui/app/composables/useOperations";
import { Button } from "#layers/ui/app/components/ui/button";
import { onDayAtTime } from "#layers/ui/app/lib/dates";

const { $t, $ts, $localeRoute: localeRoute } = useI18n();

/**
 * Backups: taking them, keeping them, and putting them back.
 *
 * An archive holds everything on the server and is always encrypted with a
 * passphrase the server does not keep. Taking and restoring run as operations
 * the server records, so this page can be left and come back to, and a second
 * administrator sees the same one running.
 */

definePageMeta({ middleware: "auth", layout: "app" });
useHead({ title: $ts("operations.title") });

const operations = useOperations();
const download = useDownload();
const { can } = useAuth();

const running = ref<Operation | null>(null);
const failure = ref<string | null>(null);
const restoring = ref<Backup | null>(null);
const deleting = ref<Backup | null>(null);
const uploaded = ref<number | null>(null);
const outcome = ref<{ ok: boolean; text: string } | null>(null);

async function refresh() {
  await operations.refresh();
  const current = operations.overview.value?.running;
  if (current && running.value?.id !== current.id) void watchOperation(current.id);
}
onMounted(refresh);

/** Follows an operation until it ends, then says how it went. */
async function watchOperation(id: string) {
  let ended: Operation | null;
  try {
    ended = await operations.follow(id, (reading) => (running.value = reading));
  } catch {
    // A restore that replaced everything ended every session, this one too.
    running.value = null;
    outcome.value = { ok: true, text: $ts("operations.restore.signedOut") };
    return;
  }
  running.value = null;
  if (ended) {
    const result = ended.result as { restartRequired?: boolean };
    outcome.value = {
      ok: ended.status === "succeeded",
      text:
        ended.status === "succeeded"
          ? result.restartRequired
            ? $ts("operations.restore.doneRestart")
            : $ts(`operations.done.${ended.kind}`)
          : (ended.error ?? $ts(`operations.status.${ended.status}`)),
    };
  }
  await refresh().catch(() => {});
}

function started(id: string) {
  outcome.value = null;
  restoring.value = null;
  void watchOperation(id);
}

async function cancel() {
  if (running.value) await operations.cancel(running.value.id).catch(() => {});
}

async function save(backup: Backup) {
  const name = `tdc-${backup.createdAt.replace(/[:]/g, "-")}.tdcbackup`;
  await download(`/api/admin/backups/${backup.id}/download`, name);
}

async function confirmDelete() {
  const backup = deleting.value;
  deleting.value = null;
  if (!backup) return;
  try {
    await operations.remove(backup.id);
  } catch (cause) {
    failure.value = isApiError(cause) ? cause.message : $ts("operations.list.deleteFailed");
  }
  await refresh();
}

async function chooseUpload(event: Event) {
  const input = event.target as HTMLInputElement;
  const file = input.files?.[0];
  input.value = "";
  if (!file || !operations.overview.value) return;
  failure.value = null;
  uploaded.value = 0;
  try {
    await operations.upload(file, operations.overview.value.maxPieceBytes, (sent) => {
      uploaded.value = Math.round((sent / file.size) * 100);
    });
  } catch (cause) {
    failure.value = isApiError(cause) ? cause.message : $ts("operations.list.uploadFailed");
  } finally {
    uploaded.value = null;
    await refresh();
  }
}

async function leaveMaintenance() {
  try {
    await operations.endMaintenance();
  } catch (cause) {
    failure.value = isApiError(cause) ? cause.message : $ts("operations.maintenance.couldNotLeave");
  }
  await refresh();
}

function size(bytes: number) {
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(0)} KB`;
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
  return `${(bytes / (1024 * 1024 * 1024)).toFixed(2)} GB`;
}
</script>

<template>
  <div class="flex-1 overflow-y-auto">
    <header class="flex h-12 shrink-0 items-center gap-3 border-b px-4">
      <NuxtLink
        :to="localeRoute({ name: 'settings' })"
        class="btn btn-ghost size-7 px-0"
        :aria-label="$ts('admin.backToSettings')"
      >
        <ArrowLeft class="size-4" />
      </NuxtLink>
      <h1 class="text-sm font-semibold">{{ $t("operations.title") }}</h1>
    </header>

    <div class="max-w-3xl space-y-8 px-4 py-6">
      <div
        v-if="operations.failed.value"
        class="flex gap-3 rounded-lg border border-destructive/30 bg-destructive/8 p-4"
        role="alert"
      >
        <ShieldAlert class="mt-0.5 size-4 shrink-0 text-destructive" />
        <p class="text-sm">{{ $t("operations.cannotSee") }}</p>
      </div>

      <template v-else-if="operations.overview.value">
        <div
          v-if="operations.overview.value.maintenance.active"
          class="flex flex-wrap items-start justify-between gap-3 rounded-lg border border-warning/30 bg-warning/8 p-3 text-sm"
          role="status"
        >
          <p class="flex gap-2">
            <TriangleAlert class="mt-0.5 size-4 shrink-0 text-warning" />
            <span>
              {{ $t("operations.maintenance.on") }}
              <span class="block text-xs text-muted-foreground">{{
                operations.overview.value.maintenance.reason
              }}</span>
            </span>
          </p>
          <Button
            v-if="can('server.restore') && !running"
            variant="outline"
            size="small"
            @click="leaveMaintenance"
          >
            {{ $t("operations.maintenance.leave") }}
          </Button>
        </div>

        <p
          v-if="failure"
          role="alert"
          class="rounded-md bg-destructive/10 px-3 py-2 text-sm text-destructive"
        >
          {{ failure }}
        </p>
        <p
          v-if="outcome"
          role="status"
          class="rounded-md px-3 py-2 text-sm"
          :class="outcome.ok ? 'bg-primary/8' : 'bg-destructive/10 text-destructive'"
        >
          {{ outcome.text }}
        </p>

        <OperationsProgress
          v-if="running"
          :operation="running"
          :cancellable="running.kind === 'backup' || running.kind === 'restore'"
          @cancel="cancel"
        />

        <section v-if="can('server.backup')">
          <h2 class="text-sm font-semibold">{{ $t("operations.create.title") }}</h2>
          <p class="mt-0.5 text-xs text-muted-foreground">{{ $t("operations.create.hint") }}</p>
          <div class="mt-3">
            <OperationsBackupCreate
              :parts="operations.overview.value.parts"
              :min-passphrase="operations.overview.value.minPassphraseLength"
              @started="started"
            />
          </div>
          <p class="mt-3 text-xs text-muted-foreground">
            {{
              operations.overview.value.scheduled && operations.overview.value.intervalHours > 0
                ? $t("operations.schedule.on", {
                    hours: operations.overview.value.intervalHours,
                    keep: operations.overview.value.keep,
                  })
                : $t("operations.schedule.off")
            }}
          </p>
        </section>

        <section>
          <div class="flex items-center justify-between gap-3">
            <h2 class="text-sm font-semibold">{{ $t("operations.list.title") }}</h2>
            <label v-if="can('server.backup')" class="btn btn-outline cursor-pointer text-xs">
              <Upload class="size-3.5" />
              {{
                uploaded === null
                  ? $t("operations.list.upload")
                  : $t("operations.list.uploading", { percent: uploaded })
              }}
              <input type="file" accept=".tdcbackup,.age" class="sr-only" @change="chooseUpload" />
            </label>
          </div>

          <p
            v-if="!operations.overview.value.backups.length"
            class="mt-3 text-sm text-muted-foreground"
          >
            {{ $t("operations.list.none") }}
          </p>
          <ul class="mt-3 space-y-2">
            <li
              v-for="backup in operations.overview.value.backups"
              :key="backup.id"
              class="rounded-lg border bg-surface px-3 py-2"
            >
              <div class="flex flex-wrap items-start justify-between gap-2">
                <div class="min-w-0 text-sm">
                  <p class="font-medium">
                    {{ onDayAtTime(backup.createdAt) }}
                    <span class="text-xs font-normal text-muted-foreground">
                      · {{ $t(`operations.origin.${backup.origin}`) }} · {{ backup.createdBy }} ·
                      {{ size(backup.sizeBytes) }}
                    </span>
                  </p>
                  <p class="text-xs text-muted-foreground">
                    <template v-if="backup.status !== 'ready'">
                      {{ $t(`operations.backupStatus.${backup.status}`) }}
                    </template>
                    <template v-else-if="backup.manifest.parts?.length">
                      {{
                        backup.manifest.parts
                          .map((part) => $ts(`operations.part.${part}`))
                          .join(", ")
                      }}
                      <template v-if="backup.manifest.rooms?.length">
                        ·
                        {{
                          $t("operations.list.someChannels", {
                            count: backup.manifest.rooms.length,
                          })
                        }}
                      </template>
                    </template>
                    <template v-else>{{ $t("operations.list.contentsUnknown") }}</template>
                  </p>
                  <p v-if="backup.error" class="text-xs text-destructive">{{ backup.error }}</p>
                </div>
                <div class="flex gap-1.5">
                  <Button
                    v-if="backup.status === 'ready' && can('server.backup')"
                    variant="ghost"
                    size="icon"
                    :label="$ts('operations.list.download')"
                    @click="save(backup)"
                  >
                    <Download class="size-4" />
                  </Button>
                  <Button
                    v-if="backup.status === 'ready' && can('server.restore')"
                    variant="outline"
                    size="small"
                    :disabled="Boolean(running)"
                    @click="restoring = restoring?.id === backup.id ? null : backup"
                  >
                    {{ $t("operations.restore.open") }}
                  </Button>
                  <Button
                    v-if="can('server.backup')"
                    variant="ghost"
                    size="icon"
                    :label="$ts('operations.list.delete')"
                    @click="deleting = backup"
                  >
                    <Trash2 class="size-4" />
                  </Button>
                </div>
              </div>
              <div v-if="restoring?.id === backup.id" class="mt-3 border-t pt-3">
                <OperationsBackupRestore
                  :backup="backup"
                  :parts="operations.overview.value.parts"
                  :min-passphrase="operations.overview.value.minPassphraseLength"
                  @started="started"
                />
              </div>
            </li>
          </ul>
        </section>

        <section v-if="operations.operations.value.length">
          <h2 class="text-sm font-semibold">{{ $t("operations.history") }}</h2>
          <ul class="mt-2 space-y-1 text-xs">
            <li v-for="op in operations.operations.value" :key="op.id">
              {{ onDayAtTime(op.requestedAt) }} · {{ $t(`operations.kind.${op.kind}`) }} ·
              {{ $t(`operations.status.${op.status}`) }} · {{ op.requestedBy }}
              <span v-if="op.error" class="text-destructive">— {{ op.error }}</span>
            </li>
          </ul>
        </section>
      </template>
    </div>
  </div>

  <ConfirmDialog
    v-if="deleting"
    :title="$ts('operations.list.deleteTitle')"
    :detail="$ts('operations.list.deleteDetail')"
    :confirm-label="$ts('operations.list.delete')"
    :cancel-label="$ts('operations.cancel')"
    @cancel="deleting = null"
    @confirm="confirmDelete"
  />
</template>
