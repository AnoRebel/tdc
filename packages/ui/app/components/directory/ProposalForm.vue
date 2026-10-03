<script setup lang="ts">
import { useForm } from "@tanstack/vue-form";
import type { AnyFieldApi } from "@tanstack/vue-form";
import * as v from "valibot";
import { Plus, Send, X } from "lucide-vue-next";

import type { ProposalVersion } from "@tdc/types";
import { isApiError } from "@tdc/types";
import { Button } from "#layers/ui/app/components/ui/button";
import { Field } from "#layers/ui/app/components/ui/field";
import { Input } from "#layers/ui/app/components/ui/input";
import { Textarea } from "#layers/ui/app/components/ui/textarea";
import { Spinner } from "#layers/ui/app/components/ui/spinner";
import { amountText, formatMoney, parseAmount } from "#layers/ui/app/lib/money";

const { $t, $ts } = useI18n();

/**
 * Writing a proposal, or the next version of one.
 *
 * A revision starts from the latest version, so changing one line means
 * changing one line rather than typing the rest again. What is sent is always
 * a new version: the one it replaces stays exactly as it was, which is what
 * lets both people see what moved between them.
 *
 * Prices are typed the way people write them and sent as whole minor units of
 * the chosen currency, converted on the digits rather than through a float.
 */

const props = defineProps<{ engagementId: string; from?: ProposalVersion }>();
const emit = defineEmits<{ sent: [ProposalVersion]; cancel: [] }>();

const directory = useDirectory();
const error = ref<string | null>(null);

function firstError(field: AnyFieldApi): string | undefined {
  if (!field.state.meta.isTouched && !form.state.submissionAttempts) return undefined;
  const [issue] = field.state.meta.errors;
  if (!issue) return undefined;
  return typeof issue === "string" ? issue : (issue as { message?: string }).message;
}

const startingCurrency = props.from?.currency ?? "TZS";
const form = useForm({
  defaultValues: {
    scope: props.from?.scope ?? "",
    timeline: props.from?.timeline ?? "",
    currency: startingCurrency,
    lineItems: props.from?.lineItems.map((item) => ({
      description: item.description,
      amount: amountText(item.amountMinor, startingCurrency),
    })) ?? [{ description: "", amount: "" }],
  },
  validators: {
    onSubmit: v.pipe(
      v.object({
        scope: v.pipe(
          v.string(),
          v.trim(),
          v.minLength(1, $ts("directory.agreement.scopeRequired")),
          v.maxLength(4000, $ts("directory.form.tooLong", { count: 4000 })),
        ),
        timeline: v.pipe(
          v.string(),
          v.trim(),
          v.minLength(1, $ts("directory.agreement.timelineRequired")),
          v.maxLength(500, $ts("directory.form.tooLong", { count: 500 })),
        ),
        currency: v.pipe(
          v.string(),
          v.trim(),
          v.toUpperCase(),
          v.regex(/^[A-Z]{3}$/, $ts("directory.agreement.currencyInvalid")),
        ),
        lineItems: v.pipe(
          v.array(
            v.object({
              description: v.pipe(
                v.string(),
                v.trim(),
                v.minLength(1, $ts("directory.agreement.itemRequired")),
                v.maxLength(300, $ts("directory.form.tooLong", { count: 300 })),
              ),
              amount: v.string(),
            }),
          ),
          v.minLength(1, $ts("directory.agreement.itemsRequired")),
          v.maxLength(30, $ts("directory.form.tooLong", { count: 30 })),
        ),
      }),
      // A price is checked against the currency chosen beside it: two decimal
      // places are right for TZS and wrong for JPY.
      v.forward(
        v.check(
          (value) =>
            value.lineItems.every(
              (item) => parseAmount(item.amount, value.currency.trim().toUpperCase()) !== null,
            ),
          $ts("directory.agreement.amountInvalid"),
        ),
        ["lineItems"],
      ),
    ),
  },
  async onSubmit({ value }) {
    error.value = null;
    const currency = value.currency.trim().toUpperCase();
    try {
      emit(
        "sent",
        await directory.propose(props.engagementId, {
          scope: value.scope.trim(),
          timeline: value.timeline.trim(),
          currency,
          lineItems: value.lineItems.map((item) => ({
            description: item.description.trim(),
            amountMinor: parseAmount(item.amount, currency) ?? 0,
          })),
        }),
      );
    } catch (cause) {
      error.value = isApiError(cause) ? cause.message : $ts("directory.couldNotReach");
    }
  },
});

// The total as it would be sent, while the form is being written.
function totalOf(values: { currency: string; lineItems: { amount: string }[] }) {
  const currency = values.currency.trim().toUpperCase();
  let sum = 0;
  for (const item of values.lineItems) {
    const minor = parseAmount(item.amount, currency);
    if (minor === null) return null;
    sum += minor;
  }
  return /^[A-Z]{3}$/.test(currency) ? formatMoney(sum, currency) : null;
}
</script>

<template>
  <form class="space-y-3" novalidate data-proposal-form @submit.prevent="form.handleSubmit()">
    <p
      v-if="error"
      class="rounded-md bg-destructive/10 px-3 py-2 text-sm text-destructive"
      role="alert"
    >
      {{ error }}
    </p>

    <form.Field name="scope">
      <template #default="{ field }">
        <Field :label="$ts('directory.agreement.scope')" :error="firstError(field)" required>
          <template #default="{ id, invalid, describedBy }">
            <Textarea
              :id="id"
              rows="4"
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

    <div class="grid gap-3 sm:grid-cols-[1fr_8rem]">
      <form.Field name="timeline">
        <template #default="{ field }">
          <Field
            :label="$ts('directory.agreement.timeline')"
            :hint="$ts('directory.agreement.timelineHint')"
            :error="firstError(field)"
            required
          >
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
      <form.Field name="currency">
        <template #default="{ field }">
          <Field
            :label="$ts('directory.agreement.currency')"
            :hint="$ts('directory.agreement.currencyHint')"
            :error="firstError(field)"
            required
          >
            <template #default="{ id, invalid, describedBy }">
              <Input
                :id="id"
                maxlength="3"
                autocapitalize="characters"
                :model-value="field.state.value"
                :invalid="invalid"
                :described-by="describedBy"
                @update:model-value="field.handleChange(String($event).toUpperCase())"
                @blur="field.handleBlur"
              />
            </template>
          </Field>
        </template>
      </form.Field>
    </div>

    <form.Field name="lineItems" mode="array">
      <template #default="{ field: items }">
        <fieldset class="space-y-2">
          <legend class="text-sm font-medium">{{ $t("directory.agreement.items") }}</legend>
          <p v-if="firstError(items)" class="text-xs text-destructive" role="alert">
            {{ firstError(items) }}
          </p>
          <div
            v-for="(_, index) in items.state.value"
            :key="index"
            class="grid grid-cols-[1fr_7rem_auto] items-end gap-2"
            data-line-item
          >
            <form.Field :name="`lineItems[${index}].description`">
              <template #default="{ field }">
                <Field
                  :label="$ts('directory.agreement.item', { number: index + 1 })"
                  :error="firstError(field)"
                  required
                >
                  <template #default="{ id, invalid, describedBy }">
                    <Input
                      :id="id"
                      :model-value="field.state.value"
                      :invalid="invalid"
                      :described-by="describedBy"
                      :placeholder="$ts('directory.agreement.itemDescription')"
                      @update:model-value="field.handleChange($event)"
                      @blur="field.handleBlur"
                    />
                  </template>
                </Field>
              </template>
            </form.Field>
            <form.Field :name="`lineItems[${index}].amount`">
              <template #default="{ field }">
                <Field :label="$ts('directory.agreement.itemAmount')" required>
                  <template #default="{ id, describedBy }">
                    <Input
                      :id="id"
                      inputmode="decimal"
                      :model-value="field.state.value"
                      :described-by="describedBy"
                      @update:model-value="field.handleChange($event)"
                      @blur="field.handleBlur"
                    />
                  </template>
                </Field>
              </template>
            </form.Field>
            <Button
              variant="ghost"
              class="size-9 px-0"
              :disabled="items.state.value.length === 1"
              :label="$ts('directory.agreement.removeItem', { number: index + 1 })"
              @click="items.removeValue(index)"
            >
              <X class="size-4" />
            </Button>
          </div>
          <div class="flex flex-wrap items-center justify-between gap-2">
            <Button
              variant="outline"
              :disabled="items.state.value.length >= 30"
              @click="items.pushValue({ description: '', amount: '' })"
            >
              <Plus class="size-3.5" />
              {{ $t("directory.agreement.addItem") }}
            </Button>
            <form.Subscribe :selector="(state) => ({ total: totalOf(state.values) })">
              <template #default="{ total }">
                <p v-if="total" class="text-sm" data-proposal-total>
                  {{ $t("directory.agreement.total") }}: <strong>{{ total }}</strong>
                </p>
              </template>
            </form.Subscribe>
          </div>
        </fieldset>
      </template>
    </form.Field>

    <form.Subscribe>
      <template #default="{ isSubmitting }">
        <div class="flex justify-end gap-2">
          <Button variant="ghost" @click="emit('cancel')">{{ $t("action.cancel") }}</Button>
          <Button type="submit" class="h-9" :disabled="isSubmitting">
            <Spinner v-if="isSubmitting" size="sm" />
            <Send v-else class="size-3.5" />
            {{ $t("directory.agreement.send") }}
          </Button>
        </div>
      </template>
    </form.Subscribe>
  </form>
</template>
