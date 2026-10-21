<script setup lang="ts">
import { Flag } from "lucide-vue-next";
import { reportReasonLabel } from "#layers/ui/app/lib/reports";
import { Button } from "#layers/ui/app/components/ui/button";
import { Textarea } from "#layers/ui/app/components/ui/textarea";

/**
 * Reporting something to a moderator.
 *
 * The reasons come from the server rather than being listed here, so a client
 * cannot offer a reason the server will refuse — and so the list stays in one
 * place when it changes.
 */

const { $t, $ts } = useI18n();

defineProps<{ body: string }>();

const emit = defineEmits<{
  cancel: [];
  report: [filing: { reason: string; detail: string }];
}>();

const api = useApi();

const reasons = ref<string[]>([]);
const reason = ref("");
const detail = ref("");

onMounted(async () => {
  try {
    reasons.value = await api.get<string[]>("/api/reports/reasons");
    reason.value = reasons.value[0] ?? "";
  } catch {
    // The dialog is unusable without them, so it says so rather than
    // presenting a form that cannot be submitted.
    reasons.value = [];
  }
});

function submit() {
  if (!reason.value) return;
  emit("report", { reason: reason.value, detail: detail.value.trim() });
}
</script>

<template>
  <div
    class="fixed inset-0 z-50 flex items-center justify-center bg-background/70 p-4 backdrop-blur-sm"
    role="dialog"
    aria-modal="true"
    :aria-label="$ts('moderation.report')"
    @click.self="emit('cancel')"
    @keydown.esc="emit('cancel')"
  >
    <div class="w-[min(26rem,100%)] rounded-xl border border-border bg-card p-4 shadow-2xl">
      <h2 class="flex items-center gap-2 text-sm font-semibold">
        <Flag class="size-4" />
        {{ $t("moderation.reportThisMessage") }}
      </h2>

      <!-- What is being reported, so the reader can see they picked the right
           message before sending it to somebody else to read. -->
      <p
        class="mt-3 max-h-20 overflow-y-auto rounded-lg bg-muted/50 px-3 py-2 text-xs text-muted-foreground"
      >
        {{ body }}
      </p>

      <p v-if="!reasons.length" class="mt-3 text-xs text-muted-foreground">
        {{ $t("moderation.reportingUnavailable") }}
      </p>

      <form v-else class="mt-4 space-y-3" @submit.prevent="submit">
        <fieldset>
          <legend class="mb-1.5 text-xs font-medium">
            {{ $t("moderation.whatIsWrongWithIt") }}
          </legend>
          <div class="space-y-1">
            <label
              v-for="option in reasons"
              :key="option"
              class="flex cursor-pointer items-center gap-2 rounded-md px-2 py-1.5 text-sm transition-colors hover:bg-accent"
            >
              <input
                v-model="reason"
                type="radio"
                :value="option"
                class="cursor-pointer accent-primary"
              />
              {{ reportReasonLabel(option, $ts) }}
            </label>
          </div>
        </fieldset>

        <div>
          <label for="report-detail" class="mb-1 block text-xs font-medium">
            {{ $t("moderation.anythingElse") }}
            <span class="text-muted-foreground">({{ $t("moderation.optional") }})</span>
          </label>
          <Textarea
            class="h-auto py-2"
            id="report-detail"
            v-model="detail"
            rows="3"
            :placeholder="$ts('moderation.whatModeratorShouldKnow')"
          />
        </div>

        <p class="text-[0.6875rem] text-muted-foreground">
          {{ $t("moderation.notAnonymousHint") }}
        </p>

        <div class="flex justify-end gap-2">
          <Button variant="ghost" @click="emit('cancel')">{{ $t("action.cancel") }}</Button>
          <Button variant="danger" type="submit" :disabled="!reason">
            {{ $t("moderation.report") }}
          </Button>
        </div>
      </form>
    </div>
  </div>
</template>
