<script setup lang="ts">
import type { PersonHit } from "@tdc/types";
import { isApiError } from "@tdc/types";
import { onKeyStroke } from "@vueuse/core";
import { Check, X } from "lucide-vue-next";

import { Avatar } from "#layers/ui/app/components/ui/avatar";
import { Button } from "#layers/ui/app/components/ui/button";
import { Spinner } from "#layers/ui/app/components/ui/spinner";

/**
 * Starting a conversation with one person or several.
 *
 * One box that collects people as somebody types, the way an address line
 * does: a name typed, a name chosen, and on to the next. With one person this
 * opens the conversation the two of them already have, if there is one; with
 * several it opens the conversation between exactly those people.
 *
 * A direct conversation is the set of people in it, so adding somebody to one
 * is starting another — the existing conversation stays as it was, with its
 * history readable by the people who were there. The dialog says so rather
 * than implying the history will follow.
 */

const { state } = useNewMessage();
const { $t, $ts, $localeRoute: localeRoute } = useI18n();
const { user } = useAuth();
const rooms = useRooms();
const router = useRouter();
const refreshRooms = inject<() => Promise<void>>("refreshRooms", async () => {});

const { query, results, searching, search, clear } = useSearch();

const chosen = ref<PersonHit[]>([]);
const active = ref(0);
const error = ref<string | null>(null);
const opening = ref(false);
const field = ref<HTMLInputElement | null>(null);

const adding = computed(() => Boolean(state.value.from));
const title = computed(() =>
  adding.value ? $ts("newMessage.addPeopleTitle") : $ts("newMessage.title"),
);

/** Who can be added: never the reader, and nobody already chosen. */
const candidates = computed(() =>
  results.value.people.filter(
    (person) => person.id !== user.value?.id && !chosen.value.some((p) => p.id === person.id),
  ),
);

watch(candidates, () => (active.value = 0));

/*
 * Each time it opens: empty, or holding the people already in the
 * conversation being added to — they are who the new one is with, plus
 * whoever is chosen next.
 */
watch(
  () => state.value.open,
  async (open) => {
    if (!open) return;
    chosen.value = [...(state.value.people ?? [])];
    error.value = null;
    clear();

    const from = state.value.from;
    if (from) {
      try {
        const members = await rooms.members(from);
        chosen.value = members
          .filter((member) => member.id !== user.value?.id)
          .map(({ id, username, displayName, address }) => ({
            id,
            username,
            displayName,
            address,
          }));
      } catch {
        // Starts empty instead; the people can be chosen by hand.
      }
    }
    await nextTick();
    field.value?.focus();
  },
  { immediate: true },
);

function close() {
  state.value = { open: false };
}

onKeyStroke("Escape", (event) => {
  if (!state.value.open) return;
  event.preventDefault();
  close();
});

function choose(person: PersonHit | undefined) {
  if (!person) return;
  chosen.value = [...chosen.value, person];
  clear();
  field.value?.focus();
}

function remove(person: PersonHit) {
  chosen.value = chosen.value.filter((p) => p.id !== person.id);
  field.value?.focus();
}

function onKeydown(event: KeyboardEvent) {
  const count = candidates.value.length;
  switch (event.key) {
    case "ArrowDown":
      event.preventDefault();
      if (count) active.value = (active.value + 1) % count;
      break;
    case "ArrowUp":
      event.preventDefault();
      if (count) active.value = (active.value - 1 + count) % count;
      break;
    case "Enter":
      event.preventDefault();
      // A name being typed is chosen; an empty box with people chosen opens
      // the conversation, so the whole thing can be done without a pointer.
      if (query.value.trim() && count) choose(candidates.value[active.value]);
      else if (!query.value.trim() && chosen.value.length) void start();
      break;
    case "Backspace":
      // Taking back the last name, as an address line does.
      if (!query.value && chosen.value.length) {
        event.preventDefault();
        chosen.value = chosen.value.slice(0, -1);
      }
      break;
  }
}

async function start() {
  if (!chosen.value.length || opening.value) return;
  opening.value = true;
  error.value = null;
  try {
    const room = await rooms.openDirect(chosen.value.map((person) => person.id));
    close();
    await refreshRooms();
    await router.push(localeRoute({ name: "rooms-id", params: { id: room.id } }));
  } catch (cause) {
    // Somebody may not accept messages from the reader. The server does not
    // say who, and neither does this.
    error.value =
      isApiError(cause) && cause.code === "forbidden"
        ? $ts("newMessage.cannotOpen")
        : $ts("newMessage.couldNotOpen");
  } finally {
    opening.value = false;
  }
}

const listId = "new-message-people";
</script>

<template>
  <Teleport to="body">
    <Transition name="motion-fade">
      <!-- eslint-disable-next-line vuejs-accessibility/no-static-element-interactions -->
      <div
        v-if="state.open"
        class="fixed inset-0 z-50 flex items-start justify-center bg-background/70 p-4 pt-[12vh] backdrop-blur-sm"
        @click.self="close"
      >
        <Transition name="motion-pop" appear>
          <div
            class="w-[min(32rem,100%)] overflow-hidden rounded-xl border border-border bg-card shadow-2xl"
            role="dialog"
            aria-modal="true"
            :aria-label="title"
          >
            <header class="flex items-center justify-between border-b px-4 py-3">
              <h2 class="text-sm font-semibold">{{ title }}</h2>
              <Button variant="ghost" size="icon" :label="$ts('action.close')" @click="close">
                <X class="size-4" aria-hidden="true" />
              </Button>
            </header>

            <div class="p-3">
              <!-- The people chosen, then the box for the next one, on one
                   line that wraps — the shape of an address line. -->
              <div
                class="flex flex-wrap items-center gap-1.5 rounded-lg border border-border bg-background px-2 py-1.5 focus-within:ring-2 focus-within:ring-ring/40"
              >
                <TransitionGroup name="motion-list">
                  <span
                    v-for="person in chosen"
                    :key="person.id"
                    class="flex items-center gap-1 rounded-md bg-accent py-0.5 pr-0.5 pl-1 text-xs"
                  >
                    <Avatar :name="person.address" size="sm" class="size-4 text-[0.5rem]" />
                    {{ person.displayName }}
                    <button
                      type="button"
                      class="grid size-4 cursor-pointer place-items-center rounded-sm text-muted-foreground hover:bg-background hover:text-foreground"
                      :aria-label="$ts('newMessage.remove', { name: person.displayName })"
                      @click="remove(person)"
                    >
                      <X class="size-3" aria-hidden="true" />
                    </button>
                  </span>
                </TransitionGroup>
                <input
                  ref="field"
                  :value="query"
                  type="text"
                  role="combobox"
                  :aria-label="$ts('newMessage.to')"
                  :aria-expanded="candidates.length > 0"
                  :aria-controls="listId"
                  :aria-activedescendant="candidates.length ? `${listId}-${active}` : undefined"
                  aria-autocomplete="list"
                  :placeholder="chosen.length ? '' : $ts('newMessage.placeholder')"
                  autocomplete="off"
                  spellcheck="false"
                  class="h-7 min-w-32 flex-1 bg-transparent text-sm outline-none placeholder:text-muted-foreground"
                  @input="
                    search(($event.target as HTMLInputElement).value, {
                      messages: false,
                      delay: 120,
                    })
                  "
                  @keydown="onKeydown"
                />
                <Spinner v-if="searching" class="size-3.5 text-muted-foreground" />
              </div>

              <ul
                :id="listId"
                role="listbox"
                :aria-label="$ts('newMessage.suggestions')"
                class="mt-2 max-h-64 overflow-y-auto"
              >
                <li
                  v-for="(person, index) in candidates"
                  :id="`${listId}-${index}`"
                  :key="person.id"
                  role="option"
                  :aria-selected="index === active"
                  class="flex cursor-pointer items-center gap-2 rounded-lg px-2 py-1.5 text-sm transition-colors"
                  :class="index === active ? 'bg-accent' : 'hover:bg-accent/60'"
                  @click="choose(person)"
                  @mousemove="active = index"
                >
                  <Avatar :name="person.address" size="sm" />
                  <span class="truncate font-medium">{{ person.displayName }}</span>
                  <span class="truncate text-xs text-muted-foreground">@{{ person.username }}</span>
                  <Check
                    v-if="index === active"
                    class="ml-auto size-3.5 text-muted-foreground"
                    aria-hidden="true"
                  />
                </li>
              </ul>

              <p
                v-if="query.trim() && !searching && !candidates.length"
                class="px-2 py-3 text-xs text-muted-foreground"
              >
                {{ $t("newMessage.nobody", { query: query.trim() }) }}
              </p>

              <p v-if="adding" class="mt-2 px-1 text-xs text-muted-foreground">
                {{ $t("newMessage.addingStartsNew") }}
              </p>

              <p v-if="error" role="alert" class="mt-2 px-1 text-xs text-destructive">
                {{ error }}
              </p>
            </div>

            <footer class="flex items-center justify-between gap-2 border-t px-4 py-3">
              <span class="text-xs text-muted-foreground">
                {{
                  chosen.length > 1
                    ? $t("newMessage.groupOf", { count: chosen.length + 1 })
                    : $t("newMessage.enterHint")
                }}
              </span>
              <span class="flex gap-2">
                <Button variant="ghost" @click="close">{{ $t("action.cancel") }}</Button>
                <Button :disabled="!chosen.length || opening" @click="start">
                  <Spinner v-if="opening" class="size-3.5" />
                  {{ $t("newMessage.start") }}
                </Button>
              </span>
            </footer>
          </div>
        </Transition>
      </div>
    </Transition>
  </Teleport>
</template>
