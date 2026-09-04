<script setup lang="ts">
import type { Message, Thread } from "@tdc/types";
import { isApiError } from "@tdc/types";
import { Bubble } from "~/components/ui/bubble";
import { Separator } from "~/components/ui/separator";

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
    error.value = isApiError(cause) ? cause.message : "Could not open this thread.";
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
    error.value = isApiError(cause) ? cause.message : "Could not post that reply.";
  } finally {
    sending.value = false;
  }
}

function bodyOf(message: Message): string {
  return message.redacted ? "This message was removed" : (message.content.body ?? "");
}

function shortName(address: string): string {
  return address.split("@")[0] ?? address;
}

function when(value: string): string {
  return new Date(value).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
}
</script>

<template>
  <aside class="flex w-full max-w-md flex-col border-l">
    <header class="flex items-center justify-between border-b px-4 py-3">
      <h2 class="text-sm font-semibold">Thread</h2>
      <button
        type="button"
        class="rounded-md border px-2 py-1 text-xs hover:bg-accent"
        @click="emit('close')"
      >
        Close
      </button>
    </header>

    <p v-if="error" class="bg-destructive/10 px-4 py-2 text-sm text-destructive" role="alert">
      {{ error }}
    </p>

    <div v-if="thread" class="flex-1 space-y-3 overflow-y-auto px-4 py-4">
      <div class="space-y-1">
        <p class="text-xs font-medium text-muted-foreground">
          {{ thread.root.sender === selfAddress ? "You" : shortName(thread.root.sender) }}
        </p>
        <Bubble :variant="thread.root.redacted ? 'redacted' : 'default'">
          {{ bodyOf(thread.root) }}
          <span class="ml-2 text-[0.6875rem] opacity-60">{{ when(thread.root.sentAt) }}</span>
        </Bubble>
      </div>

      <Separator
        :label="thread.replies.length === 1 ? '1 reply' : `${thread.replies.length} replies`"
      />

      <div v-for="reply in thread.replies" :key="reply.id" class="space-y-1">
        <p class="text-xs font-medium text-muted-foreground">
          {{ reply.sender === selfAddress ? "You" : shortName(reply.sender) }}
        </p>
        <Bubble
          :variant="reply.redacted ? 'redacted' : reply.sender === selfAddress ? 'mine' : 'default'"
        >
          {{ bodyOf(reply) }}
          <span class="ml-2 text-[0.6875rem] opacity-60">{{ when(reply.sentAt) }}</span>
        </Bubble>
      </div>
    </div>

    <footer class="border-t px-4 py-3">
      <form class="flex gap-2" @submit.prevent="send">
        <input
          v-model="draft"
          placeholder="Reply in thread"
          class="flex-1 rounded-md border bg-background px-3 py-2 text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring"
          autocomplete="off"
        />
        <button
          type="submit"
          :disabled="sending || !draft.trim()"
          class="rounded-md bg-primary px-3 py-2 text-sm font-medium text-primary-foreground disabled:opacity-50"
        >
          Reply
        </button>
      </form>
    </footer>
  </aside>
</template>
