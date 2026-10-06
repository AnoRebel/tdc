<script setup lang="ts">
import type { PersonHit, RoomHit } from "@tdc/types";
import type { Command } from "#layers/ui/app/composables/useCommands";
import { Hash, Lock, MessageSquare, Search, User } from "lucide-vue-next";
import { Avatar } from "#layers/ui/app/components/ui/avatar";
import { Kbd } from "#layers/ui/app/components/ui/kbd";
import { Spinner } from "#layers/ui/app/components/ui/spinner";

/**
 * Search across conversations, people and messages.
 *
 * An overlay rather than a page, because searching is nearly always a way of
 * getting somewhere: the answer to "where was that" is a jump, and going to a
 * results page first puts a stop in the middle of it.
 */

const open = defineModel<boolean>({ default: false });

const { query, results, searching, failed, total, isEmpty, search, clear } = useSearch();

const emit = defineEmits<{ shortcuts: []; createChannel: [] }>();

const { matching } = useCommands({
  openShortcuts: () => emit("shortcuts"),
  createChannel: () => emit("createChannel"),
});
const rooms = useRooms();
const router = useRouter();
const { $t, $ts, $localeRoute: localeRoute } = useI18n();

const field = ref<HTMLInputElement>();
const active = ref(0);
const joining = ref<string>();

/**
 * Every result in one list, in the order they are shown.
 *
 * Flattened so the arrow keys move through what the eye sees. Keeping three
 * lists and a section index would mean the keyboard and the display could
 * disagree about what is selected, which is exactly the bug that makes a
 * palette feel unreliable.
 */
type Entry =
  | { kind: "command"; command: Command }
  | { kind: "room"; room: RoomHit }
  | { kind: "person"; person: PersonHit }
  | { kind: "message"; index: number };

/*
 * Commands share this box rather than having one of their own.
 *
 * Two palettes on adjacent shortcuts is two things to learn and a decision to
 * make before typing — and the decision is one nobody wants to make, because
 * "take me to the audit log" and "find the audit log" are the same intention.
 * One box answers both.
 *
 * Commands come first: they are matched locally and appear as somebody types,
 * where search results arrive after a round trip. Putting them below would
 * mean the list reordering under the selection as the answer arrives.
 */
const commands = computed(() => matching(query.value));

const entries = computed<Entry[]>(() => [
  ...commands.value.map((command) => ({ kind: "command" as const, command })),
  ...results.value.rooms.map((room) => ({ kind: "room" as const, room })),
  ...results.value.people.map((person) => ({ kind: "person" as const, person })),
  ...results.value.messages.map((_, index) => ({ kind: "message" as const, index })),
]);

// Back to the top whenever the results change, so the selection is never left
// pointing at a row that has been replaced by a different one.
watch(entries, () => {
  active.value = 0;
});

watch(open, async (isOpen) => {
  if (!isOpen) {
    clear();
    return;
  }
  await nextTick();
  field.value?.focus();
});

function onInput(event: Event) {
  search((event.target as HTMLInputElement).value);
}

function move(delta: number) {
  if (!entries.value.length) return;
  // Wraps, so holding an arrow key never dead-ends at either edge.
  active.value = (active.value + delta + entries.value.length) % entries.value.length;
}

async function choose(entry: Entry | undefined) {
  if (!entry) return;

  if (entry.kind === "command") {
    // Closed first: a command that navigates would otherwise leave the palette
    // covering wherever it went.
    open.value = false;
    await entry.command.run();
    return;
  }

  if (entry.kind === "room") {
    await openRoom(entry.room);
    return;
  }

  if (entry.kind === "person") {
    await openDirect(entry.person);
    return;
  }

  const message = results.value.messages[entry.index];
  if (!message) return;

  // Carries the message so the room can scroll to it and mark it, rather than
  // landing at the bottom and leaving the reader to find what they searched
  // for by hand.
  //
  // The counter makes each jump a distinct location. Without it, searching the
  // same message twice leaves the query identical, the room does not notice,
  // and the second attempt does nothing — which reads as the search being
  // broken rather than as already being there.
  open.value = false;
  await router.push(
    localeRoute({
      name: "rooms-id",
      params: { id: message.roomId },
      query: { at: message.eventId, jump: String(Date.now()) },
    }),
  );
}

/** Opens a room, joining first when the person is not yet in it. */
async function openRoom(room: RoomHit) {
  if (!room.member) {
    joining.value = room.id;
    try {
      await rooms.join(room.id);
    } catch {
      // Joining can fail — an invitation may have been withdrawn between the
      // search and the click. The room is still opened: a public room is
      // readable without membership, and the room itself explains what is
      // possible far better than an error in a palette that is about to close.
    } finally {
      joining.value = undefined;
    }
  }

  open.value = false;
  await router.push(localeRoute({ name: "rooms-id", params: { id: room.id } }));
}

async function openDirect(person: PersonHit) {
  joining.value = person.id;
  try {
    const room = await rooms.openDirect([person.id]);
    open.value = false;
    await router.push(localeRoute({ name: "rooms-id", params: { id: room.id } }));
  } catch {
    // Somebody may not accept direct messages. The palette stays open with the
    // results still showing, so the attempt is visibly not a navigation.
    failed.value = true;
  } finally {
    joining.value = undefined;
  }
}

function onKeydown(event: KeyboardEvent) {
  switch (event.key) {
    case "ArrowDown":
      event.preventDefault();
      move(1);
      break;
    case "ArrowUp":
      event.preventDefault();
      move(-1);
      break;
    case "Enter":
      event.preventDefault();
      void choose(entries.value[active.value]);
      break;
    case "Escape":
      event.preventDefault();
      open.value = false;
      break;
  }
}

/** The index a section's first row occupies in the flattened list. */
/*
 * Where each section begins in the flattened list.
 *
 * Every one derives from the section before it, including the first — which
 * used to be implicitly zero, so adding a section above it silently put two
 * rows under the same index and highlighted both.
 */
const commandsFrom = computed(() => 0);
const roomsFrom = computed(() => commandsFrom.value + commands.value.length);
const peopleFrom = computed(() => roomsFrom.value + results.value.rooms.length);
const messagesFrom = computed(() => peopleFrom.value + results.value.people.length);

function iconFor(room: RoomHit) {
  return room.visibility === "private" ? Lock : Hash;
}
</script>

<template>
  <Teleport to="body">
    <Transition name="motion-fade">
      <div
        v-if="open"
        class="fixed inset-0 z-50 bg-background/70 backdrop-blur-sm"
        @click="open = false"
      >
        <!-- Held above centre: the list grows downwards, and a centred panel
             would shift the input under the reader's eye as results arrive. -->
        <div
          class="motion-pop-in mx-auto mt-[12vh] w-[min(40rem,calc(100vw-2rem))] overflow-hidden rounded-xl border border-border bg-card shadow-2xl"
          role="dialog"
          aria-modal="true"
          :aria-label="$ts('nav.search')"
          @click.stop
        >
          <div class="flex items-center gap-2 border-b border-border px-3">
            <Search class="size-4 shrink-0 text-muted-foreground" />
            <input
              ref="field"
              :value="query"
              type="text"
              :placeholder="$ts('nav.searchPlaceholder')"
              class="h-12 flex-1 bg-transparent text-sm outline-none placeholder:text-muted-foreground"
              :aria-label="$ts('nav.searchPlaceholder')"
              autocomplete="off"
              spellcheck="false"
              @input="onInput"
              @keydown="onKeydown"
            />
            <Spinner v-if="searching" class="size-4 text-muted-foreground" />
            <Kbd v-else>esc</Kbd>
          </div>

          <div class="max-h-[60vh] overflow-y-auto p-1.5">
            <!--
              Commands, which are matched here rather than on the server and so
              are shown before anything has been typed. That is what makes the
              palette browsable: somebody who cannot remember what it can do
              opens it and reads.
            -->
            <section v-if="commands.length">
              <h2
                class="px-2 pt-1.5 pb-1 text-[0.6875rem] font-semibold tracking-wide text-muted-foreground uppercase"
              >
                {{ $t("nav.actions") }}
              </h2>
              <button
                v-for="(command, index) in commands"
                :key="command.id"
                type="button"
                class="flex w-full items-center gap-2.5 rounded-md px-2 py-2 text-left transition-colors"
                :class="active === commandsFrom + index ? 'bg-accent' : 'hover:bg-accent/60'"
                @click="choose({ kind: 'command', command })"
                @mousemove="active = commandsFrom + index"
              >
                <component :is="command.icon" class="size-4 shrink-0 text-muted-foreground" />
                <span class="min-w-0 flex-1 truncate text-sm">{{ command.label }}</span>
              </button>
            </section>

            <p
              v-if="!query.trim() && !commands.length"
              class="px-2 py-8 text-center text-xs text-muted-foreground"
            >
              {{ $t("nav.searchHint") }}
            </p>

            <p v-else-if="failed" class="px-2 py-8 text-center text-xs text-muted-foreground">
              {{ $t("nav.searchFailed") }}
            </p>

            <!-- Only when the commands found nothing either: a query matching
                 an action but no message has not come up empty. -->
            <p
              v-else-if="isEmpty && !commands.length"
              class="px-2 py-8 text-center text-xs text-muted-foreground"
            >
              {{ $t("nav.noMatches", { query: query.trim() }) }}
            </p>

            <template v-else-if="total">
              <section v-if="results.rooms.length">
                <h2
                  class="px-2 pt-1.5 pb-1 text-[0.6875rem] font-semibold tracking-wide text-muted-foreground uppercase"
                >
                  {{ $t("nav.channels") }}
                </h2>
                <button
                  v-for="(room, index) in results.rooms"
                  :key="room.id"
                  type="button"
                  class="palette-row"
                  :data-active="active === roomsFrom + index"
                  @click="choose({ kind: 'room', room })"
                  @mousemove="active = roomsFrom + index"
                >
                  <component :is="iconFor(room)" class="size-4 shrink-0 text-muted-foreground" />
                  <span class="truncate font-medium">{{ room.name }}</span>
                  <span v-if="room.topic" class="truncate text-xs text-muted-foreground">
                    {{ room.topic }}
                  </span>
                  <span class="ml-auto flex shrink-0 items-center gap-1.5">
                    <Spinner v-if="joining === room.id" class="size-3.5" />
                    <span v-else-if="!room.member" class="text-[0.6875rem] text-muted-foreground">
                      {{ $t("action.join") }}
                    </span>
                    <span
                      v-if="room.archived"
                      class="rounded bg-muted px-1.5 py-0.5 text-[0.6875rem] text-muted-foreground"
                    >
                      {{ $t("nav.archived") }}
                    </span>
                  </span>
                </button>
              </section>

              <section v-if="results.people.length">
                <h2
                  class="px-2 pt-2 pb-1 text-[0.6875rem] font-semibold tracking-wide text-muted-foreground uppercase"
                >
                  {{ $t("nav.people") }}
                </h2>
                <button
                  v-for="(person, index) in results.people"
                  :key="person.id"
                  type="button"
                  class="palette-row"
                  :data-active="active === peopleFrom + index"
                  @click="choose({ kind: 'person', person })"
                  @mousemove="active = peopleFrom + index"
                >
                  <Avatar :name="person.address" size="sm" class="shrink-0" />
                  <span class="truncate font-medium">{{ person.displayName }}</span>
                  <span class="truncate text-xs text-muted-foreground">@{{ person.username }}</span>
                  <Spinner v-if="joining === person.id" class="ml-auto size-3.5 shrink-0" />
                  <User v-else class="ml-auto size-3.5 shrink-0 text-muted-foreground" />
                </button>
              </section>

              <section v-if="results.messages.length">
                <h2
                  class="px-2 pt-2 pb-1 text-[0.6875rem] font-semibold tracking-wide text-muted-foreground uppercase"
                >
                  {{ $t("nav.messages") }}
                </h2>
                <button
                  v-for="(message, index) in results.messages"
                  :key="message.eventId"
                  type="button"
                  class="palette-row items-start"
                  :data-active="active === messagesFrom + index"
                  @click="choose({ kind: 'message', index })"
                  @mousemove="active = messagesFrom + index"
                >
                  <MessageSquare class="mt-0.5 size-4 shrink-0 text-muted-foreground" />
                  <span class="min-w-0 flex-1 text-left">
                    <span class="block truncate text-xs text-muted-foreground">
                      {{ message.sender }}
                    </span>
                    <!-- The server's marks are kept, but the text still goes
                         through the same sanitiser as any message: a client is
                         not where the decision about what is safe belongs. -->
                    <SearchExcerpt :html="message.highlighted || message.body" />
                  </span>
                </button>
              </section>
            </template>
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<style scoped>
@reference "#layers/ui/app/assets/css/main.css";

.palette-row {
  @apply flex w-full cursor-pointer items-center gap-2 rounded-lg px-2 py-2 text-left text-sm transition-colors;
}

.palette-row[data-active="true"] {
  @apply bg-accent text-accent-foreground;
}
</style>
