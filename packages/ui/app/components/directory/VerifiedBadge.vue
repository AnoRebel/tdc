<script setup lang="ts">
import type { Verified } from "@tdc/types";
import { BadgeCheck } from "lucide-vue-next";
import { onDate } from "#layers/ui/app/lib/dates";

const { $t } = useI18n();

/**
 * That one claim was verified — and exactly what that means.
 *
 * The only place a verification is drawn, so the statement of its meaning is
 * written once and cannot drift from screen to screen: a moderator checked the
 * evidence for this one claim and judged it credible, and that is all. Not the
 * person, not their work, not whether they will deliver. A verified badge that
 * could be read as an endorsement is exactly how somebody ends up trusting a
 * stranger further than anybody checked.
 *
 * The meaning is behind a toggle rather than always open, because a profile
 * with several verified claims would otherwise say it several times; the
 * toggle is a real button, so it is reachable and announced.
 */

const props = defineProps<{ verified: Verified }>();

const open = ref(false);
const detailId = useId();
</script>

<template>
  <span class="inline-flex flex-col items-start gap-1" data-verified>
    <button
      type="button"
      class="inline-flex items-center gap-1 rounded-full border border-success/40 bg-success/10 px-2 py-0.5 text-[0.6875rem] font-medium text-foreground focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
      :aria-expanded="open"
      :aria-controls="detailId"
      @click="open = !open"
    >
      <BadgeCheck class="size-3.5 text-success" />
      {{ $t("directory.verification.verified") }}
    </button>
    <span
      v-if="open"
      :id="detailId"
      class="block max-w-prose space-y-1 rounded-md bg-muted/60 px-2 py-1.5 text-xs font-normal text-muted-foreground"
    >
      <span class="block" data-verification-meaning>
        {{
          $t("directory.verification.meaning", {
            by: props.verified.by,
            date: onDate(props.verified.at),
          })
        }}
      </span>
      <!-- Only the owner is sent the note, so only the owner sees this. -->
      <span v-if="props.verified.note" class="block" data-verification-note>
        {{ $t("directory.verification.whatWasChecked", { note: props.verified.note }) }}
      </span>
    </span>
  </span>
</template>
