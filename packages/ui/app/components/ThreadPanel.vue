<script setup lang="ts">
import type { Message, Thread } from "@tdc/types";
import { isApiError } from "@tdc/types";
import { SendHorizontal, X } from "lucide-vue-next";
import { Avatar } from "#layers/ui/app/components/ui/avatar";
import { Bubble } from "#layers/ui/app/components/ui/bubble";
import { Separator } from "#layers/ui/app/components/ui/separator";
import { Skeleton } from "#layers/ui/app/components/ui/skeleton";
import { Spinner } from "#layers/ui/app/components/ui/spinner";
import { Button } from "#layers/ui/app/components/ui/button";
import { atTime } from "#layers/ui/app/lib/dates";

const { $t, $ts } = useI18n();

/**
 * A thread, opened beside the channel.
 *
 * Threads inherit the channel's permissions, so there is nothing to check
 * here: a person who can see this panel can already read the channel.
 */

const props = defineProps<{
  roomId: string;
  rootId: string;
  /** The reader's own address, for telling their messages from others'. */
  selfAddress: string;
}>();

const emit = defineEmits<{ close: []; replied: [] }>();

const roomsApi = useRooms();

const thread = ref<Thread | null>(null);
const draft = ref("");
const error = ref<string | null>(null);
const sending = ref(false);

async function load() {
  try {
    thread.value = await roomsApi.thread(props.roomId, props.rootId);
  } catch (cause) {
    error.value = isApiError(cause) ? cause.message : $ts("room.couldNotOpenThread");
  }
}

watch(() => props.rootId, load, { immediate: true });

async function send() {
  const body = draft.value.trim();
  if (!body || sending.value) return;

  sending.value = true;
  const previous = draft.value;
  draft.value = "";

  try {
    await roomsApi.reply(props.roomId, props.rootId, body, crypto.randomUUID());
    await load();
    emit("replied");
  } catch (cause) {
    // The text is restored, so a failure does not lose what was typed.
    draft.value = previous;
    error.value = isApiError(cause) ? cause.message : $ts("room.couldNotPostReply");
  } finally {
    sending.value = false;
  }
}

function bodyOf(message: Message): string {
  return message.redacted ? $ts("room.messageRemoved") : (message.content.body ?? "");
}

function shortName(address: string): string {
  return address.split("@")[0] ?? address;
}

function when(value: string): string {
  return atTime(value);
}
</script>

<template>
  <aside class="flex w-80 shrink-0 flex-col border-l bg-surface-sunken">
    <header class="flex h-12 shrink-0 items-center gap-2 border-b px-3">
      <h2 class="text-sm font-semibold">{{ $t("room.thread") }}</h2>
      <span v-if="thread" class="text-xs text-muted-foreground">
        {{
          thread.replies.length === 1
            ? $t("room.oneReply")
            : $t("room.manyReplies", { count: thread.replies.length })
        }}
      </span>
      <Button
        variant="ghost"
        size="icon"
        class="ml-auto"
        :label="$ts('room.closeThread')"
        @click="emit('close')"
      >
        <X class="size-4" />
      </Button>
    </header>

    <p
      v-if="error"
      class="shrink-0 bg-destructive/10 px-3 py-2 text-sm text-destructive"
      role="alert"
    >
      {{ error }}
    </p>

    <div class="flex-1 space-y-4 overflow-y-auto px-3 py-3">
      <template v-if="thread">
        <!-- The message the thread hangs off, set apart so it reads as the
             subject rather than the first reply. -->
        <article class="rounded-lg border bg-surface p-3">
          <div class="flex gap-2.5">
            <Avatar :name="thread.root.sender" size="sm" class="mt-0.5" />
            <div class="min-w-0 flex-1">
              <p class="flex items-baseline gap-2">
                <span class="text-xs font-semibold">
                  {{
                    thread.root.sender === selfAddress
                      ? $t("room.you")
                      : shortName(thread.root.sender)
                  }}
                </span>
                <time class="text-[0.6875rem] text-muted-foreground">
                  {{ when(thread.root.sentAt) }}
                </time>
              </p>
              <Bubble :variant="thread.root.redacted ? 'redacted' : 'default'" class="max-w-full">
                <MessageBody :body="bodyOf(thread.root)" />
              </Bubble>
            </div>
          </div>
        </article>

        <Separator
          v-if="thread.replies.length"
          :label="
            thread.replies.length === 1
              ? $ts('room.oneReply')
              : $ts('room.manyReplies', { count: thread.replies.length })
          "
        />

        <p v-else class="py-6 text-center text-xs text-muted-foreground">
          {{ $t("room.noRepliesYet") }}
        </p>

        <article v-for="reply in thread.replies" :key="reply.id" class="flex gap-2.5 px-1">
          <Avatar :name="reply.sender" size="sm" class="mt-0.5" />
          <div class="min-w-0 flex-1">
            <p class="mb-0.5 flex items-baseline gap-2 leading-tight">
              <span class="text-xs font-semibold">
                {{ reply.sender === selfAddress ? $t("room.you") : shortName(reply.sender) }}
              </span>
              <time class="text-[0.6875rem] text-muted-foreground">{{ when(reply.sentAt) }}</time>
            </p>
            <Bubble
              :variant="
                reply.redacted ? 'redacted' : reply.sender === selfAddress ? 'mine' : 'default'
              "
              class="max-w-full"
            >
              <MessageBody :body="bodyOf(reply)" />
            </Bubble>
          </div>
        </article>
      </template>

      <div v-else class="space-y-4">
        <Skeleton class="h-16 rounded-lg" />
        <Skeleton class="h-10 w-3/4" />
      </div>
    </div>

    <footer class="shrink-0 px-3 pt-1 pb-4">
      <form
        class="flex items-end gap-2 rounded-xl border bg-surface p-1.5 shadow-xs transition focus-within:border-ring"
        @submit.prevent="send"
      >
        <input
          v-model="draft"
          :placeholder="$ts('action.reply')"
          class="min-w-0 flex-1 bg-transparent px-2 py-1.5 text-sm outline-none placeholder:text-muted-foreground"
          autocomplete="off"
        />
        <Button
          size="icon"
          class="shrink-0"
          type="submit"
          :disabled="sending || !draft.trim()"
          :title="$ts('action.reply')"
        >
          <Spinner v-if="sending" size="sm" />
          <SendHorizontal v-else class="size-4" />
        </Button>
      </form>
    </footer>
  </aside>
</template>
