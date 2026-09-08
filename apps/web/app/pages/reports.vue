<script setup lang="ts">
import type { Report } from "@tdc/types";
import { ArrowLeft, Check, Flag, ShieldAlert, X } from "lucide-vue-next";
import { Avatar } from "#layers/ui/app/components/ui/avatar";
import { reportReasonLabel } from "~/lib/reports";

/**
 * The moderation queue.
 *
 * Reaching this needs the review permission, which the server checks on the
 * request; the check below only decides what to show. Somebody who navigates
 * here without it sees the refusal rather than an empty queue, because an
 * empty queue and a refused one look the same and mean opposite things.
 *
 * Everything a moderator needs to decide is on the card: what was said, who
 * said it, why it was reported and by whom. The reported party has no way to
 * see any of it — this page is the only place the reporter's name appears.
 */

definePageMeta({ middleware: "auth", layout: "app" });

useHead({ title: "Reports" });

const queue = useReportQueue();

/*
 * Resolving asks for a note first.
 *
 * A queue where anything can be cleared with one click is a queue that gets
 * cleared without being read. The note is what the next moderator sees when
 * the same person is reported again.
 */
const deciding = ref<{ report: Report; dismissing: boolean } | null>(null);
const note = ref("");
const busy = ref(false);

onMounted(() => void queue.refresh());

function decide(report: Report, dismissing: boolean) {
  deciding.value = { report, dismissing };
  note.value = "";
}

async function record() {
  if (!deciding.value) return;

  const { report, dismissing } = deciding.value;
  busy.value = true;

  try {
    if (dismissing) {
      await queue.dismiss(report.id, note.value.trim());
    } else {
      await queue.resolve(report.id, note.value.trim());
    }
    deciding.value = null;
  } catch {
    // The list is refreshed either way; a failure leaves the report where it
    // was rather than removing it from a screen it is still open on.
    await queue.refresh();
  } finally {
    busy.value = false;
  }
}

function when(value: number): string {
  return new Date(value).toLocaleString([], {
    day: "numeric",
    month: "short",
    hour: "2-digit",
    minute: "2-digit",
  });
}

/** The local part of an address, which is what a person is called here. */
function name(address: string): string {
  return address.split("@")[0] ?? address;
}
</script>

<template>
  <div class="flex-1 overflow-y-auto">
    <header class="flex h-12 shrink-0 items-center gap-3 border-b px-4">
      <NuxtLink to="/settings" class="btn btn-ghost size-7 px-0" aria-label="Back to settings">
        <ArrowLeft class="size-4" />
      </NuxtLink>
      <h1 class="text-sm font-semibold">Reports</h1>
      <span
        v-if="!queue.failed.value && queue.reports.value.length"
        class="tabular ml-auto text-xs text-muted-foreground"
      >
        {{ queue.reports.value.length }} open
      </span>
    </header>

    <div class="max-w-3xl px-4 py-6">
      <div
        v-if="queue.failed.value"
        class="flex gap-3 rounded-lg border border-destructive/30 bg-destructive/8 p-4"
        role="alert"
      >
        <ShieldAlert class="mt-0.5 size-4 shrink-0 text-destructive" />
        <div class="text-sm">
          <p class="font-medium text-destructive">You cannot read the moderation queue.</p>
          <p class="mt-1 text-muted-foreground">
            This needs the
            <code class="rounded bg-muted px-1 py-0.5 font-mono text-xs">moderation.review</code>
            permission.
          </p>
        </div>
      </div>

      <template v-else>
        <p class="text-sm text-muted-foreground">
          What people have reported, oldest first. The person reported is never told who reported
          them.
        </p>

        <!-- An empty queue is the good outcome, so it says so rather than
             looking like something failed to load. -->
        <div
          v-if="!queue.loading.value && !queue.reports.value.length"
          class="mt-6 rounded-lg border border-dashed py-12 text-center"
        >
          <Flag class="mx-auto size-6 text-muted-foreground/60" />
          <p class="mt-2 text-sm font-medium">Nothing waiting</p>
          <p class="mt-0.5 text-xs text-muted-foreground">Every report has been dealt with.</p>
        </div>

        <ul class="mt-4 space-y-3">
          <li
            v-for="report in queue.reports.value"
            :key="report.id"
            class="rounded-lg border bg-surface p-4"
          >
            <div class="flex flex-wrap items-start gap-3">
              <Avatar :name="name(report.subject)" size="sm" />

              <div class="min-w-0 flex-1">
                <p class="text-sm">
                  <span class="font-medium">{{ name(report.subject) }}</span>
                  <span class="text-muted-foreground"> was reported for </span>
                  <span class="font-medium">{{ reportReasonLabel(report.reason) }}</span>
                </p>
                <p class="mt-0.5 text-xs text-muted-foreground">
                  <template v-if="report.reporter"> by {{ name(report.reporter) }} · </template>
                  <time>{{ when(report.createdAt) }}</time>
                </p>
              </div>
            </div>

            <!-- A copy taken when the report was filed, so it survives the
                 author deleting what they said.

                 Inline-block so it hugs a short message: stretched to the full
                 width of the card, three words read as an empty input rather
                 than as something somebody said. -->
            <blockquote
              v-if="report.reportedBody"
              class="mt-3 inline-block max-h-40 max-w-full overflow-y-auto rounded-lg border-l-2 border-muted-foreground/40 bg-muted/40 px-3 py-2 text-sm break-words whitespace-pre-wrap"
            >
              {{ report.reportedBody }}
            </blockquote>

            <p v-if="report.detail" class="mt-2 text-sm text-muted-foreground">
              <span class="font-medium">They said:</span> {{ report.detail }}
            </p>

            <div class="mt-3 flex flex-wrap gap-2">
              <button type="button" class="btn btn-outline" @click="decide(report, false)">
                <Check class="size-3.5" />
                Acted on it
              </button>
              <button type="button" class="btn btn-ghost" @click="decide(report, true)">
                <X class="size-3.5" />
                Dismiss
              </button>
              <NuxtLink
                v-if="report.roomId"
                :to="`/rooms/${report.roomId}`"
                class="btn btn-ghost ml-auto"
              >
                Open the channel
              </NuxtLink>
            </div>
          </li>
        </ul>
      </template>
    </div>

    <div
      v-if="deciding"
      class="fixed inset-0 z-50 flex items-center justify-center bg-background/70 p-4 backdrop-blur-sm"
      role="dialog"
      aria-modal="true"
      :aria-label="deciding.dismissing ? 'Dismiss this report' : 'Record what was done'"
      @click.self="deciding = null"
      @keydown.esc="deciding = null"
    >
      <div class="w-[min(26rem,100%)] rounded-xl border border-border bg-card p-4 shadow-2xl">
        <h2 class="text-sm font-semibold">
          {{ deciding.dismissing ? "Dismiss this report" : "Record what was done" }}
        </h2>
        <p class="mt-1 text-xs text-muted-foreground">
          {{
            deciding.dismissing
              ? "Kept on the record as considered and not acted on."
              : "The next moderator to see this person reported will read this."
          }}
        </p>

        <form class="mt-3 space-y-3" @submit.prevent="record">
          <textarea
            v-model="note"
            rows="3"
            class="input h-auto py-2"
            :placeholder="
              deciding.dismissing ? 'Why this needs nothing done' : 'What you did about it'
            "
          />

          <div class="flex justify-end gap-2">
            <button type="button" class="btn btn-ghost" @click="deciding = null">Cancel</button>
            <button type="submit" class="btn btn-primary" :disabled="busy">
              {{ busy ? "Recording" : deciding.dismissing ? "Dismiss" : "Record" }}
            </button>
          </div>
        </form>
      </div>
    </div>
  </div>
</template>
