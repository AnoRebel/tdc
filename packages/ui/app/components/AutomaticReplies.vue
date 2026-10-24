<script setup lang="ts">
import { nameOf } from "#layers/ui/app/lib/names";
import { isApiError } from "@tdc/types";
import type { AutomaticReply, AutomaticReplyInput } from "@tdc/types";
import { useForm } from "@tanstack/vue-form";
import { MessageSquareReply, Pencil, Plus, Trash2 } from "lucide-vue-next";
import * as v from "valibot";

import { Avatar } from "#layers/ui/app/components/ui/avatar";
import { Button } from "#layers/ui/app/components/ui/button";
import { Checkbox } from "#layers/ui/app/components/ui/checkbox";
import { Field } from "#layers/ui/app/components/ui/field";
import { Input } from "#layers/ui/app/components/ui/input";
import { Select } from "#layers/ui/app/components/ui/select";
import { Switch } from "#layers/ui/app/components/ui/switch";
import { Textarea } from "#layers/ui/app/components/ui/textarea";
import { fully, wallClockIn, wallClockOf } from "#layers/ui/app/lib/dates";
import { automaticReplyLimit } from "#layers/ui/app/composables/useAutomaticReplies";

/**
 * Automatic replies, in settings: what the server says on somebody's behalf
 * while they are away, when it says it, and what it looks like to the person
 * who receives it.
 *
 * The preview is the reply exactly as a conversation draws it — the name, the
 * mark that says it was automatic, the formatted text — because the question
 * somebody writing one is asking is "how will this read to them?".
 */

const { $t, $ts } = useI18n();
const automatic = useAutomaticReplies();
const status = useOwnStatus();
const { user } = useAuth();
const { say } = useNotice();

const error = ref<string | null>(null);
/** The reply being edited: an id, "new", or nothing. */
const editing = ref<string | "new" | null>(null);
const deleting = ref<AutomaticReply | null>(null);
const previewing = ref<"direct" | "mention">("direct");

const here = deviceTimeZone();

onMounted(async () => {
  try {
    await Promise.all([automatic.load(), status.refresh()]);
  } catch (cause) {
    error.value = isApiError(cause) ? cause.message : $ts("autoReply.couldNotLoad");
  }
});

/** Statuses a reply can be tied to: all but the default, which means "here". */
const statusChoices = computed(() =>
  status.statuses.value.filter((option) => option.key !== "available"),
);

const zones = computed(() => {
  let all: string[] = [];
  try {
    all = Intl.supportedValuesOf("timeZone");
  } catch {
    // Older runtimes: this device's zone and UTC are still offered.
  }
  return [...new Set([here, "UTC", ...all])].map((zone) => ({ value: zone, label: zone }));
});

const limitMessage = computed(() => $ts("autoReply.tooLong", { limit: automaticReplyLimit }));

const schema = computed(() =>
  v.pipe(
    v.object({
      enabled: v.boolean(),
      message: v.pipe(
        v.string(),
        v.trim(),
        v.minLength(1, $ts("autoReply.required")),
        v.maxLength(automaticReplyLimit, limitMessage.value),
      ),
      separateMention: v.boolean(),
      mentionMessage: v.pipe(
        v.string(),
        v.trim(),
        v.maxLength(automaticReplyLimit, limitMessage.value),
      ),
      statuses: v.array(v.string()),
      from: v.string(),
      until: v.string(),
      timeZone: v.pipe(v.string(), v.minLength(1)),
      replyToDirect: v.boolean(),
      replyToGroups: v.boolean(),
      replyToMentions: v.boolean(),
    }),
    v.forward(
      v.check((value) => {
        const from = wallClockIn(value.from, value.timeZone);
        const until = wallClockIn(value.until, value.timeZone);
        return !from || !until || from < until;
      }, $ts("autoReply.endsBeforeStart")),
      ["until"],
    ),
    v.forward(
      v.check(
        (value) => value.replyToDirect || value.replyToGroups || value.replyToMentions,
        $ts("autoReply.answersNothing"),
      ),
      ["replyToDirect"],
    ),
  ),
);

const blank = () => ({
  enabled: true,
  message: "",
  separateMention: false,
  mentionMessage: "",
  statuses: [] as string[],
  from: "",
  until: "",
  timeZone: here,
  replyToDirect: true,
  replyToGroups: false,
  replyToMentions: true,
});

type Draft = ReturnType<typeof blank>;

function inputOf(value: Draft): AutomaticReplyInput {
  return {
    enabled: value.enabled,
    message: value.message.trim(),
    mentionMessage: value.separateMention ? value.mentionMessage.trim() : "",
    statuses: value.statuses,
    startsAt: wallClockIn(value.from, value.timeZone),
    endsAt: wallClockIn(value.until, value.timeZone),
    timeZone: value.timeZone,
    replyToDirect: value.replyToDirect,
    replyToGroups: value.replyToGroups,
    replyToMentions: value.replyToMentions,
  };
}

/** A stored reply as a change to it would send it, unchanged. */
function inputFrom(reply: AutomaticReply): AutomaticReplyInput {
  const { id: _id, active: _active, updatedAt: _updatedAt, ...input } = reply;
  return input;
}

const form = useForm({
  defaultValues: blank(),
  validators: { onSubmit: schema.value },
  async onSubmit({ value }) {
    error.value = null;
    try {
      const target = editing.value;
      if (target && target !== "new") await automatic.update(target, inputOf(value));
      else await automatic.create(inputOf(value));
      editing.value = null;
      say($ts("autoReply.saved"));
    } catch (cause) {
      error.value = isApiError(cause) ? cause.message : $ts("autoReply.couldNotSave");
    }
  },
});

function startNew() {
  error.value = null;
  form.reset(blank());
  previewing.value = "direct";
  editing.value = "new";
}

function startEditing(reply: AutomaticReply) {
  error.value = null;
  form.reset({
    enabled: reply.enabled,
    message: reply.message,
    separateMention: Boolean(reply.mentionMessage),
    mentionMessage: reply.mentionMessage,
    statuses: [...reply.statuses],
    from: reply.startsAt ? wallClockOf(reply.startsAt, reply.timeZone) : "",
    until: reply.endsAt ? wallClockOf(reply.endsAt, reply.timeZone) : "",
    timeZone: reply.timeZone || here,
    replyToDirect: reply.replyToDirect,
    replyToGroups: reply.replyToGroups,
    replyToMentions: reply.replyToMentions,
  });
  previewing.value = "direct";
  editing.value = reply.id;
}

async function toggle(reply: AutomaticReply, enabled: boolean) {
  error.value = null;
  try {
    await automatic.update(reply.id, { ...inputFrom(reply), enabled });
  } catch (cause) {
    error.value = isApiError(cause) ? cause.message : $ts("autoReply.couldNotSave");
  }
}

async function confirmDelete() {
  const reply = deleting.value;
  deleting.value = null;
  if (!reply) return;
  try {
    await automatic.remove(reply.id);
    if (editing.value === reply.id) editing.value = null;
    say($ts("autoReply.deleted"));
  } catch (cause) {
    error.value = isApiError(cause) ? cause.message : $ts("autoReply.couldNotSave");
  }
}

/** When a reply answers, said in a line. */
function whenItAnswers(reply: AutomaticReply): string {
  const which = reply.statuses.length
    ? $ts("autoReply.whileStatus", {
        statuses: reply.statuses.map((key) => status.labelOf(key)).join(", "),
      })
    : $ts("autoReply.anyStatus");
  const from = reply.startsAt ? fully(reply.startsAt, reply.timeZone) : null;
  const until = reply.endsAt ? fully(reply.endsAt, reply.timeZone) : null;
  const window =
    from && until
      ? $ts("autoReply.window", { from, until })
      : from
        ? $ts("autoReply.fromOnly", { from })
        : until
          ? $ts("autoReply.untilOnly", { until })
          : "";
  return [which, window && `${window} (${reply.timeZone})`].filter(Boolean).join(" · ");
}

function errorOf(errors: unknown[]): string | undefined {
  const first = errors[0] as { message?: string } | string | undefined;
  if (!first) return undefined;
  return typeof first === "string" ? first : first.message;
}

function toggleStatus(current: string[], key: string, on: boolean): string[] {
  return on ? [...new Set([...current, key])] : current.filter((value) => value !== key);
}
</script>

<template>
  <section aria-labelledby="automatic-replies-heading" data-automatic-replies>
    <div class="flex flex-wrap items-start justify-between gap-2">
      <div class="min-w-0">
        <h2 id="automatic-replies-heading" class="flex items-center gap-1.5 text-sm font-semibold">
          <MessageSquareReply class="size-4" />
          {{ $t("autoReply.title") }}
        </h2>
        <p class="mt-1 text-xs text-muted-foreground">{{ $t("autoReply.hint") }}</p>
      </div>
      <Button v-if="editing === null" variant="outline" @click="startNew">
        <Plus class="size-3.5" />
        {{ $t("autoReply.new") }}
      </Button>
    </div>

    <p
      v-if="error"
      role="alert"
      class="mt-3 rounded-md bg-destructive/10 px-3 py-2 text-xs text-destructive"
    >
      {{ error }}
    </p>

    <p
      v-if="automatic.replies.value && !automatic.replies.value.length && editing === null"
      class="mt-3 text-xs text-muted-foreground"
    >
      {{ $t("autoReply.none") }}
    </p>

    <ul v-if="automatic.replies.value?.length" class="mt-3 space-y-1.5">
      <li
        v-for="reply in automatic.replies.value"
        :key="reply.id"
        class="rounded-lg border bg-surface px-3 py-2.5"
        :class="editing === reply.id && 'border-primary'"
      >
        <div class="flex items-start gap-3">
          <div class="min-w-0 flex-1">
            <p class="flex flex-wrap items-center gap-1.5 text-xs">
              <span
                class="rounded px-1.5 py-0.5 text-[0.6875rem] font-medium"
                :class="
                  reply.active
                    ? 'bg-success/15 text-success-text'
                    : 'bg-muted text-muted-foreground'
                "
              >
                {{
                  reply.active
                    ? $t("autoReply.onNow")
                    : reply.enabled
                      ? $t("autoReply.waiting")
                      : $t("autoReply.off")
                }}
              </span>
              <span class="text-muted-foreground">{{ whenItAnswers(reply) }}</span>
            </p>
            <p class="mt-1 line-clamp-2 text-sm break-words">{{ reply.message }}</p>
          </div>
          <Switch
            :model-value="reply.enabled"
            :label="$ts('autoReply.enabled')"
            @update:model-value="toggle(reply, $event)"
          />
          <Button
            variant="ghost"
            size="icon"
            :label="$ts('autoReply.edit')"
            @click="startEditing(reply)"
          >
            <Pencil class="size-3.5" />
          </Button>
          <Button
            variant="danger"
            size="icon"
            :label="$ts('autoReply.delete')"
            @click="deleting = reply"
          >
            <Trash2 class="size-3.5" />
          </Button>
        </div>
      </li>
    </ul>

    <Transition name="motion-fade">
      <form
        v-if="editing !== null"
        class="mt-3 grid gap-4 rounded-xl border bg-card p-4 md:grid-cols-[minmax(0,1fr)_minmax(0,16rem)]"
        :aria-label="editing === 'new' ? $ts('autoReply.new') : $ts('autoReply.edit')"
        @submit.prevent="form.handleSubmit()"
      >
        <div class="min-w-0 space-y-4">
          <form.Field name="message">
            <template #default="{ field }">
              <Field
                :label="$ts('autoReply.message')"
                :hint="$ts('autoReply.messageHint', { limit: automaticReplyLimit })"
                :error="errorOf(field.state.meta.errors)"
              >
                <template #default="{ id, invalid, describedBy }">
                  <Textarea
                    :id="id"
                    :model-value="field.state.value"
                    rows="3"
                    :placeholder="$ts('autoReply.messagePlaceholder')"
                    :invalid="invalid"
                    :described-by="describedBy"
                    @update:model-value="field.handleChange($event)"
                  />
                  <p
                    class="mt-0.5 text-right text-[0.6875rem] tabular-nums"
                    :class="
                      field.state.value.length > automaticReplyLimit
                        ? 'text-destructive'
                        : 'text-muted-foreground'
                    "
                    aria-hidden="true"
                  >
                    {{ field.state.value.length }}/{{ automaticReplyLimit }}
                  </p>
                </template>
              </Field>
            </template>
          </form.Field>

          <form.Field name="separateMention">
            <template #default="{ field }">
              <label class="flex cursor-pointer items-center gap-2 text-sm">
                <Checkbox
                  :model-value="field.state.value"
                  @update:model-value="field.handleChange($event)"
                />
                {{ $t("autoReply.separateMention") }}
              </label>
              <form.Field v-if="field.state.value" name="mentionMessage">
                <template #default="{ field: mention }">
                  <Field
                    class="mt-2"
                    :label="$ts('autoReply.mentionMessage')"
                    :error="errorOf(mention.state.meta.errors)"
                  >
                    <template #default="{ id, invalid, describedBy }">
                      <Textarea
                        :id="id"
                        :model-value="mention.state.value"
                        rows="2"
                        :invalid="invalid"
                        :described-by="describedBy"
                        @update:model-value="mention.handleChange($event)"
                      />
                    </template>
                  </Field>
                </template>
              </form.Field>
            </template>
          </form.Field>

          <fieldset class="space-y-3">
            <legend class="text-xs font-semibold">{{ $t("autoReply.when") }}</legend>

            <form.Field name="statuses">
              <template #default="{ field }">
                <fieldset>
                  <legend class="mb-1 text-xs font-medium">{{ $t("autoReply.statuses") }}</legend>
                  <div class="flex flex-wrap gap-1.5">
                    <label
                      v-for="option in statusChoices"
                      :key="option.key"
                      class="flex cursor-pointer items-center gap-1.5 rounded-lg border px-2.5 py-1.5 text-sm has-[:checked]:border-primary has-[:checked]:bg-primary/8"
                    >
                      <Checkbox
                        :model-value="field.state.value.includes(option.key)"
                        @update:model-value="
                          field.handleChange(toggleStatus(field.state.value, option.key, $event))
                        "
                      />
                      {{ option.label }}
                    </label>
                  </div>
                  <p class="mt-1 text-xs text-muted-foreground">
                    {{ $t("autoReply.statusesHint") }}
                  </p>
                </fieldset>
              </template>
            </form.Field>

            <div class="grid gap-3 sm:grid-cols-2">
              <form.Field name="from">
                <template #default="{ field }">
                  <Field :label="$ts('autoReply.from')">
                    <template #default="{ id }">
                      <Input
                        :id="id"
                        type="datetime-local"
                        :model-value="field.state.value"
                        @update:model-value="field.handleChange($event)"
                      />
                    </template>
                  </Field>
                </template>
              </form.Field>
              <form.Field name="until">
                <template #default="{ field }">
                  <Field
                    :label="$ts('autoReply.until')"
                    :hint="$ts('autoReply.untilHint')"
                    :error="errorOf(field.state.meta.errors)"
                  >
                    <template #default="{ id, invalid, describedBy }">
                      <Input
                        :id="id"
                        type="datetime-local"
                        :model-value="field.state.value"
                        :invalid="invalid"
                        :described-by="describedBy"
                        @update:model-value="field.handleChange($event)"
                      />
                    </template>
                  </Field>
                </template>
              </form.Field>
            </div>

            <form.Field name="timeZone">
              <template #default="{ field }">
                <Field :label="$ts('autoReply.zone')">
                  <template #default="{ id }">
                    <Select
                      :id="id"
                      :model-value="field.state.value"
                      :label="$ts('autoReply.zone')"
                      :options="zones"
                      @update:model-value="$event !== undefined && field.handleChange($event)"
                    />
                  </template>
                </Field>
              </template>
            </form.Field>
          </fieldset>

          <form.Field name="replyToDirect">
            <template #default="{ field: direct }">
              <fieldset>
                <legend class="mb-1 text-xs font-semibold">{{ $t("autoReply.answers") }}</legend>
                <div class="space-y-1">
                  <label class="flex cursor-pointer items-center gap-2 text-sm">
                    <Checkbox
                      :model-value="direct.state.value"
                      @update:model-value="direct.handleChange($event)"
                    />
                    {{ $t("autoReply.direct") }}
                  </label>
                  <form.Field name="replyToGroups">
                    <template #default="{ field }">
                      <label class="flex cursor-pointer items-center gap-2 text-sm">
                        <Checkbox
                          :model-value="field.state.value"
                          @update:model-value="field.handleChange($event)"
                        />
                        {{ $t("autoReply.groups") }}
                      </label>
                    </template>
                  </form.Field>
                  <form.Field name="replyToMentions">
                    <template #default="{ field }">
                      <label class="flex cursor-pointer items-center gap-2 text-sm">
                        <Checkbox
                          :model-value="field.state.value"
                          @update:model-value="field.handleChange($event)"
                        />
                        {{ $t("autoReply.mentions") }}
                      </label>
                    </template>
                  </form.Field>
                </div>
                <p
                  v-if="errorOf(direct.state.meta.errors)"
                  role="alert"
                  class="mt-1 text-xs text-destructive"
                >
                  {{ errorOf(direct.state.meta.errors) }}
                </p>
              </fieldset>
            </template>
          </form.Field>

          <form.Field name="enabled">
            <template #default="{ field }">
              <label class="flex cursor-pointer items-center gap-2 text-sm">
                <Switch
                  :model-value="field.state.value"
                  @update:model-value="field.handleChange($event)"
                />
                {{ $t("autoReply.enabled") }}
              </label>
            </template>
          </form.Field>

          <p class="text-[0.6875rem] text-muted-foreground">{{ $t("autoReply.privacyNote") }}</p>

          <div class="flex justify-end gap-2">
            <Button variant="ghost" @click="editing = null">{{ $t("action.cancel") }}</Button>
            <Button type="submit">{{ $t("action.save") }}</Button>
          </div>
        </div>

        <!-- The reply as a conversation draws it, for the two places it can
             appear. Live, so what is typed is what is shown. -->
        <form.Subscribe>
          <template #default="{ values }">
            <div
              class="min-w-0 md:sticky md:top-4 md:self-start"
              role="region"
              :aria-label="$ts('autoReply.preview')"
              data-preview
            >
              <p class="mb-1.5 text-xs font-semibold">{{ $t("autoReply.preview") }}</p>
              <div class="mb-2 flex gap-1" role="group">
                <Button
                  v-for="where in ['direct', 'mention'] as const"
                  :key="where"
                  size="small"
                  :variant="previewing === where ? 'primary' : 'ghost'"
                  :aria-pressed="previewing === where"
                  @click="previewing = where"
                >
                  {{
                    where === "direct"
                      ? $t("autoReply.previewDirect")
                      : $t("autoReply.previewMention")
                  }}
                </Button>
              </div>
              <div class="rounded-lg border bg-background p-3">
                <div class="flex items-center gap-2">
                  <Avatar :name="user ? nameOf(user) : ''" size="sm" />
                  <span class="text-xs font-semibold">{{ user && nameOf(user) }}</span>
                  <span
                    class="rounded border border-border px-1 text-[0.625rem] font-medium tracking-wide text-muted-foreground uppercase"
                  >
                    {{ $t("autoReply.badge") }}
                  </span>
                </div>
                <div class="mt-1.5 text-sm break-words">
                  <MessageBody
                    :body="
                      (previewing === 'mention' &&
                      values.separateMention &&
                      values.mentionMessage.trim()
                        ? values.mentionMessage
                        : values.message
                      ).trim() || $ts('autoReply.messagePlaceholder')
                    "
                  />
                </div>
              </div>
            </div>
          </template>
        </form.Subscribe>
      </form>
    </Transition>

    <ConfirmDialog
      v-if="deleting"
      :title="$ts('autoReply.deleteTitle')"
      :detail="$ts('autoReply.deleteDetail')"
      :confirm-label="$ts('autoReply.delete')"
      @cancel="deleting = null"
      @confirm="confirmDelete"
    />
  </section>
</template>
