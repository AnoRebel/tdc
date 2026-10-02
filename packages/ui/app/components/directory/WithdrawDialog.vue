<script setup lang="ts">
import { useForm } from "@tanstack/vue-form";
import * as v from "valibot";
import { Button } from "#layers/ui/app/components/ui/button";
import { Textarea } from "#layers/ui/app/components/ui/textarea";

const { $t, $ts } = useI18n();

/**
 * Withdrawing from an engagement, asked first.
 *
 * It cannot be undone — a withdrawn engagement is closed for good, and its
 * conversation stops taking messages — so the dialog says so, and offers room
 * for a reason the other person will see in the engagement's history.
 */

const emit = defineEmits<{ cancel: []; confirm: [reason: string] }>();

const reasonId = useId();
const form = useForm({
  defaultValues: { reason: "" },
  validators: {
    onSubmit: v.object({
      reason: v.pipe(v.string(), v.maxLength(500, $ts("directory.form.tooLong", { count: 500 }))),
    }),
  },
  onSubmit({ value }) {
    emit("confirm", value.reason.trim());
  },
});
</script>

<template>
  <!-- eslint-disable-next-line vuejs-accessibility/no-static-element-interactions -- a dialog backdrop, dismissed by Escape as well -->
  <div
    class="fixed inset-0 z-50 flex items-center justify-center bg-background/70 p-4 backdrop-blur-sm"
    role="dialog"
    aria-modal="true"
    :aria-label="$ts('directory.engagement.withdrawTitle')"
    @click.self="emit('cancel')"
    @keydown.esc="emit('cancel')"
  >
    <form
      class="w-[min(26rem,100%)] space-y-3 rounded-xl border bg-card p-4 shadow-2xl"
      @submit.prevent="form.handleSubmit()"
    >
      <h2 class="text-sm font-semibold">{{ $t("directory.engagement.withdrawTitle") }}</h2>
      <p class="text-xs text-muted-foreground">{{ $t("directory.engagement.withdrawDetail") }}</p>
      <form.Field name="reason">
        <template #default="{ field }">
          <label class="sr-only" :for="reasonId">{{
            $t("directory.engagement.withdrawWhy")
          }}</label>
          <Textarea
            :id="reasonId"
            rows="3"
            :model-value="field.state.value"
            :placeholder="$ts('directory.engagement.withdrawWhy')"
            @update:model-value="field.handleChange($event)"
          />
        </template>
      </form.Field>
      <div class="flex justify-end gap-2">
        <Button variant="ghost" @click="emit('cancel')">{{ $t("action.cancel") }}</Button>
        <Button type="submit" variant="danger">{{ $t("directory.engagement.withdraw") }}</Button>
      </div>
    </form>
  </div>
</template>
