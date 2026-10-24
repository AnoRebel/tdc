<script setup lang="ts">
import type { Room, ScheduledMessage } from "@tdc/types";
import { isApiError } from "@tdc/types";
import { onKeyStroke } from "@vueuse/core";
import { CalendarClock, Clock, Pencil, SendHorizontal, Trash2, X } from "lucide-vue-next";

import { Button } from "#layers/ui/app/components/ui/button";
import { Textarea } from "#layers/ui/app/components/ui/textarea";
import { howLongAgo, onWeekdayAtTime } from "#layers/ui/app/lib/dates";
import { nameOf } from "#layers/ui/app/lib/names";
import { otherParticipants } from "#layers/ui/app/lib/sidebar";

/**
 * Messages written now and waiting to be sent.
 *
 * Every one can be reworded, moved, sent at once or withdrawn — a scheduled
 * message somebody cannot call back is one they stop trusting the feature
 * with. Opened from a conversation it shows that conversation's, with the
 * rest one press away.
 */

const props = defineProps<{
  /** The conversations somebody is in, to name where each message is going. */
  rooms: Room[];
}>();

const { $t, $ts, $localeRoute: localeRoute } = useI18n();
const { user } = useAuth();
const router = useRouter();
const scheduledMessages = { ...useScheduled(), ...useScheduledChanges() };
const { scheduled, showing } = scheduledMessages;
const { say } = useNotice();

const onlyHere = ref(true);
const editing = ref<string | null>(null);
const editDraft = ref("");
const moving = ref<ScheduledMessage | null>(null);
const cancelling = ref<ScheduledMessage | null>(null);
const busy = ref<string | null>(null);
const error = ref<string | null>(null);

const roomId = computed(() => showing.value.roomId);
const shown = computed(() =>
  roomId.value && onlyHere.value
    ? scheduled.value.filter((item) => item.roomId === roomId.value)
    : scheduled.value,
);

watch(
  () => showing.value.open,
  (open) => {
    if (!open) return;
    onlyHere.value = true;
    editing.value = null;
    error.value = null;
    void scheduledMessages.refresh();
  },
  { immediate: true },
);

function close() {
  showing.value = { open: false };
}

onKeyStroke("Escape", (event) => {
  // The dialogs this opens close first.
  if (!showing.value.open || moving.value || cancelling.value) return;
  event.preventDefault();
  if (editing.value) editing.value = null;
  else close();
});

function roomNameOf(id: string): string {
  const room = props.rooms.find((candidate) => candidate.id === id);
  if (!room) return $ts("scheduled.aConversation");
  return room.kind === "direct"
    ? otherParticipants(room, user.value ? nameOf(user.value) : undefined).join(", ")
    : `#${room.name}`;
}

/** Runs one change against one message, saying what went wrong if it does. */
async function act(item: ScheduledMessage, change: () => Promise<unknown>, done: string) {
  busy.value = item.id;
  error.value = null;
  try {
    await change();
    say(done);
  } catch (cause) {
    error.value = isApiError(cause) ? cause.message : $ts("scheduled.couldNotChange");
  } finally {
    busy.value = null;
  }
}

function startEditing(item: ScheduledMessage) {
  editing.value = item.id;
  editDraft.value = item.body;
}

function saveEdit(item: ScheduledMessage) {
  const body = editDraft.value.trim();
  if (!body) return;
  editing.value = null;
  void act(item, () => scheduledMessages.change(item.id, { body }), $ts("scheduled.saved"));
}

function move(sendAt: Date) {
  const item = moving.value;
  moving.value = null;
  if (item)
    void act(item, () => scheduledMessages.change(item.id, { sendAt }), $ts("scheduled.moved"));
}

function sendNow(item: ScheduledMessage) {
  void act(item, () => scheduledMessages.sendNow(item.id), $ts("scheduled.sent"));
}

function confirmCancel() {
  const item = cancelling.value;
  cancelling.value = null;
  if (item) void act(item, () => scheduledMessages.cancel(item.id), $ts("scheduled.cancelled"));
}

async function goTo(id: string) {
  close();
  await router.push(localeRoute({ name: "rooms-id", params: { id } }));
}
</script>

<template>
  <Teleport to="body">
    <Transition name="motion-fade">
      <!-- eslint-disable-next-line vuejs-accessibility/no-static-element-interactions -->
      <div
        v-if="showing.open"
        class="fixed inset-0 z-50 flex items-start justify-center bg-background/70 p-4 pt-[10vh] backdrop-blur-sm"
        @click.self="close"
      >
        <Transition name="motion-pop" appear>
          <div
            class="flex max-h-[80dvh] w-[min(34rem,100%)] flex-col overflow-hidden rounded-xl border border-border bg-card shadow-2xl"
            role="dialog"
            aria-modal="true"
            :aria-label="$ts('scheduled.title')"
          >
            <header class="flex items-center gap-2 border-b px-4 py-3">
              <CalendarClock class="size-4 text-muted-foreground" aria-hidden="true" />
              <h2 class="text-sm font-semibold">{{ $t("scheduled.title") }}</h2>
              <!-- Only from a conversation, where "here" means something. -->
              <div
                v-if="roomId"
                class="ml-auto flex rounded-lg border p-0.5 text-xs"
                role="group"
                :aria-label="$ts('scheduled.which')"
              >
                <button
                  type="button"
                  class="cursor-pointer rounded-md px-2 py-0.5 transition-colors"
                  :class="onlyHere ? 'bg-accent font-medium' : 'text-muted-foreground'"
                  :aria-pressed="onlyHere"
                  @click="onlyHere = true"
                >
                  {{ $t("scheduled.here") }}
                </button>
                <button
                  type="button"
                  class="cursor-pointer rounded-md px-2 py-0.5 transition-colors"
                  :class="!onlyHere ? 'bg-accent font-medium' : 'text-muted-foreground'"
                  :aria-pressed="!onlyHere"
                  @click="onlyHere = false"
                >
                  {{ $t("scheduled.everywhere", { count: scheduled.length }) }}
                </button>
              </div>
              <Button
                variant="ghost"
                size="icon"
                :class="!roomId && 'ml-auto'"
                :label="$ts('action.close')"
                @click="close"
              >
                <X class="size-4" aria-hidden="true" />
              </Button>
            </header>

            <p
              v-if="error"
              role="alert"
              class="border-b bg-destructive/10 px-4 py-2 text-xs text-destructive"
            >
              {{ error }}
            </p>

            <div class="min-h-0 flex-1 overflow-y-auto p-2">
              <p v-if="!shown.length" class="px-2 py-10 text-center text-sm text-muted-foreground">
                {{ $t("scheduled.nothing") }}
              </p>

              <TransitionGroup v-else tag="ul" name="motion-list" class="relative space-y-1">
                <li
                  v-for="item in shown"
                  :key="item.id"
                  class="group rounded-lg px-3 py-2.5 transition-colors hover:bg-accent/50"
                >
                  <div class="flex items-center gap-2 text-xs text-muted-foreground">
                    <Clock class="size-3.5 shrink-0" aria-hidden="true" />
                    <time
                      :datetime="new Date(item.sendAt).toISOString()"
                      class="font-medium text-foreground"
                    >
                      {{ onWeekdayAtTime(item.sendAt) }}
                    </time>
                    <span>· {{ $t("scheduled.inTime", { time: howLongAgo(item.sendAt) }) }}</span>
                    <button
                      v-if="!roomId || !onlyHere"
                      type="button"
                      class="ml-auto cursor-pointer truncate hover:text-foreground hover:underline"
                      @click="goTo(item.roomId)"
                    >
                      {{ roomNameOf(item.roomId) }}
                    </button>
                  </div>

                  <form
                    v-if="editing === item.id"
                    class="mt-2 space-y-2"
                    @submit.prevent="saveEdit(item)"
                  >
                    <Textarea
                      v-model="editDraft"
                      rows="3"
                      :label="$ts('scheduled.wording')"
                      autofocus
                      @keydown.enter.exact.prevent="saveEdit(item)"
                    />
                    <div class="flex justify-end gap-2">
                      <Button variant="ghost" @click="editing = null">{{
                        $t("action.cancel")
                      }}</Button>
                      <Button type="submit" :disabled="!editDraft.trim()">{{
                        $t("action.save")
                      }}</Button>
                    </div>
                  </form>

                  <template v-else>
                    <p class="mt-1 line-clamp-3 text-sm break-words whitespace-pre-wrap">
                      {{ item.body }}
                    </p>
                    <p v-if="item.attachments?.length" class="mt-0.5 text-xs text-muted-foreground">
                      {{ $t("scheduled.withFiles", { count: item.attachments.length }) }}
                    </p>

                    <div class="mt-2 flex flex-wrap gap-1">
                      <Button
                        variant="ghost"
                        class="h-7 px-2 text-xs"
                        :disabled="busy === item.id"
                        @click="sendNow(item)"
                      >
                        <SendHorizontal class="size-3.5" aria-hidden="true" />
                        {{ $t("scheduled.sendNow") }}
                      </Button>
                      <Button
                        variant="ghost"
                        class="h-7 px-2 text-xs"
                        :disabled="busy === item.id"
                        @click="moving = item"
                      >
                        <CalendarClock class="size-3.5" aria-hidden="true" />
                        {{ $t("scheduled.changeTime") }}
                      </Button>
                      <Button
                        variant="ghost"
                        class="h-7 px-2 text-xs"
                        :disabled="busy === item.id"
                        @click="startEditing(item)"
                      >
                        <Pencil class="size-3.5" aria-hidden="true" />
                        {{ $t("action.edit") }}
                      </Button>
                      <Button
                        variant="ghost"
                        class="h-7 px-2 text-xs text-destructive"
                        :disabled="busy === item.id"
                        @click="cancelling = item"
                      >
                        <Trash2 class="size-3.5" aria-hidden="true" />
                        {{ $t("scheduled.cancel") }}
                      </Button>
                    </div>
                  </template>
                </li>
              </TransitionGroup>
            </div>
          </div>
        </Transition>
      </div>
    </Transition>

    <ScheduleDialog v-if="moving" :body="moving.body" @cancel="moving = null" @schedule="move" />

    <ConfirmDialog
      v-if="cancelling"
      :title="$ts('scheduled.cancelTitle')"
      :detail="cancelling.body"
      :confirm-label="$ts('scheduled.cancel')"
      :cancel-label="$ts('scheduled.keep')"
      @confirm="confirmCancel"
      @cancel="cancelling = null"
    />
  </Teleport>
</template>
