<script setup lang="ts">
import {
  isApiError,
  type Board,
  type BoardCard,
  type BoardColumn,
  type BoardFilter,
} from "@tdc/types";
import { useWindowFocus } from "@vueuse/core";
import { Avatar } from "#layers/ui/app/components/ui/avatar";
import { Button } from "#layers/ui/app/components/ui/button";
import LabelChip from "#layers/ui/app/components/projects/LabelChip.vue";
import PriorityBadge from "#layers/ui/app/components/projects/PriorityBadge.vue";

const { $t, $ts, $localeRoute: localeRoute } = useI18n();

/**
 * A project's issues as columns, one per state.
 *
 * Not a table: a board is cards in columns, each column its own page of the
 * state's issues, fetched a page at a time as somebody reaches the end of it.
 * The columns are the project's states and nothing else, so adding or
 * reordering a state is the board changing.
 *
 * A card moves two ways, and they are one code path. Dragged with a pointer,
 * or picked up from the keyboard — its move button, then the arrow keys to
 * choose a column and Enter to put it there — both end in moveCard, which
 * sends the state the card was seen in. If somebody else moved it first the
 * server refuses, and the board reloads to show where it really is rather than
 * where either of them guessed.
 */
const props = defineProps<{
  projectKey: string;
  canMove: boolean;
  /** Narrows every column: by label, priority or assignee. */
  filter?: BoardFilter;
}>();

const projectsApi = useProjects();
const board = ref<Board | null>(null);
const failed = ref<string | null>(null);
/** What a screen reader is told, as moves happen. */
const announcement = ref("");

async function load() {
  failed.value = null;
  try {
    board.value = await projectsApi.board(props.projectKey, props.filter);
  } catch (cause) {
    failed.value = isApiError(cause) ? cause.message : $ts("projects.couldNotLoad");
  }
}
onMounted(load);
watch(() => [props.projectKey, props.filter], load, { deep: true });

// Somebody coming back to the tab sees the board as it is now, not as they
// left it: other people move cards too.
const focused = useWindowFocus();
watch(focused, (now) => {
  if (now && !moving.value) void load();
});

async function more(column: BoardColumn) {
  if (!column.next) return;
  try {
    const page = await projectsApi.column(
      props.projectKey,
      column.stateId,
      column.next,
      props.filter,
    );
    column.cards.push(...page.cards);
    column.next = page.next;
  } catch (cause) {
    failed.value = isApiError(cause) ? cause.message : $ts("projects.couldNotLoad");
  }
}

function columnOf(key: string): BoardColumn | undefined {
  return board.value?.columns.find((column) => column.cards.some((card) => card.key === key));
}

const moving = ref(false);

/*
 * The one way a card moves. Applied at once, so the board answers the hand
 * that moved it; then the server decides. A refusal because somebody else
 * moved it first reloads the board, which is where it actually went.
 */
async function moveCard(card: BoardCard, to: BoardColumn) {
  const from = columnOf(card.key);
  if (!from || from.stateId === to.stateId) return;

  from.cards = from.cards.filter((existing) => existing.key !== card.key);
  from.total--;
  to.cards.unshift(card);
  to.total++;
  moving.value = true;
  failed.value = null;
  try {
    await projectsApi.move(card.key, to.stateId, from.stateId);
    announcement.value = $ts("projects.board.moved", { issue: card.key, state: to.name });
  } catch (cause) {
    if (isApiError(cause) && cause.status === 409) {
      announcement.value = $ts("projects.board.movedMeanwhile", { issue: card.key });
      failed.value = $ts("projects.board.movedMeanwhile", { issue: card.key });
    } else {
      failed.value = isApiError(cause) ? cause.message : $ts("projects.couldNotMove");
      announcement.value = failed.value;
    }
    await load();
  } finally {
    moving.value = false;
  }
}

/* Dragging with a pointer. */
const dragged = ref<BoardCard | null>(null);
const over = ref<string | null>(null);

function onDragStart(event: DragEvent, card: BoardCard) {
  dragged.value = card;
  event.dataTransfer?.setData("text/plain", card.key);
  if (event.dataTransfer) event.dataTransfer.effectAllowed = "move";
}
function onDragEnd() {
  dragged.value = null;
  over.value = null;
}
function onDrop(column: BoardColumn) {
  const card = dragged.value;
  over.value = null;
  dragged.value = null;
  if (card) void moveCard(card, column);
}

/* Moving from the keyboard: pick up, choose a column, put down. */
const held = ref<{ card: BoardCard; target: number } | null>(null);
const columnIndex = (stateId: string) =>
  board.value?.columns.findIndex((column) => column.stateId === stateId) ?? -1;

function pickUp(card: BoardCard) {
  const from = columnOf(card.key);
  if (!from) return;
  held.value = { card, target: columnIndex(from.stateId) };
  announcement.value = $ts("projects.board.pickedUp", { issue: card.key, state: from.name });
}

function onHeldKey(event: KeyboardEvent) {
  const holding = held.value;
  const columns = board.value?.columns;
  if (!holding || !columns) return;

  if (event.key === "ArrowRight" || event.key === "ArrowLeft") {
    event.preventDefault();
    const step = event.key === "ArrowRight" ? 1 : -1;
    holding.target = Math.min(columns.length - 1, Math.max(0, holding.target + step));
    announcement.value = $ts("projects.board.over", { state: columns[holding.target]!.name });
  } else if (event.key === "Enter" || event.key === " ") {
    event.preventDefault();
    const target = columns[holding.target]!;
    held.value = null;
    void moveCard(holding.card, target).then(async () => {
      // Focus follows the card to its new column, so the next move starts
      // from where this one ended.
      await nextTick();
      document.getElementById(`move-${holding.card.key}`)?.focus();
    });
  } else if (event.key === "Escape") {
    event.preventDefault();
    held.value = null;
    announcement.value = $ts("projects.board.cancelled", { issue: holding.card.key });
  }
}

function isTarget(index: number) {
  return held.value?.target === index;
}
</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col">
    <p class="sr-only" aria-live="assertive">{{ announcement }}</p>
    <p
      v-if="failed"
      class="mx-4 mt-3 rounded-md bg-destructive/10 px-3 py-2 text-sm text-destructive"
      role="alert"
    >
      {{ failed }}
    </p>
    <p id="board-instructions" class="sr-only">{{ $t("projects.board.instructions") }}</p>

    <div v-if="!board" class="flex gap-3 p-4" aria-busy="true">
      <UiSkeleton v-for="n in 3" :key="n" class="h-64 w-72 shrink-0 rounded-xl" />
    </div>

    <div
      v-else
      class="flex min-h-0 flex-1 gap-3 overflow-x-auto p-4"
      role="list"
      :aria-label="$ts('projects.board.columns')"
    >
      <section
        v-for="(column, index) in board.columns"
        :key="column.stateId"
        role="listitem"
        class="flex max-h-full w-72 shrink-0 flex-col rounded-xl border bg-surface-sunken transition-colors"
        :class="
          over === column.stateId || isTarget(index) ? 'border-primary ring-2 ring-primary/30' : ''
        "
        :aria-label="$ts('projects.board.column', { state: column.name, count: column.total })"
        :data-state="column.name"
        @dragover.prevent="over = column.stateId"
        @dragleave="over = over === column.stateId ? null : over"
        @drop.prevent="onDrop(column)"
      >
        <header class="flex items-center gap-2 px-3 py-2">
          <Icon
            :name="column.isCompletion ? 'lucide:circle-check' : 'lucide:circle-dashed'"
            class="size-3.5 shrink-0"
            :class="column.isCompletion ? 'text-success-text' : 'text-muted-foreground'"
            aria-hidden="true"
          />
          <h2 class="truncate text-xs font-semibold">{{ column.name }}</h2>
          <span class="tabular ml-auto text-xs text-muted-foreground">{{ column.total }}</span>
        </header>

        <ol class="min-h-12 flex-1 space-y-1.5 overflow-y-auto px-2 pb-2">
          <li
            v-for="card in column.cards"
            :key="card.key"
            class="group rounded-lg border bg-card p-2.5 shadow-xs transition-shadow hover:shadow-sm"
            :class="[
              dragged?.key === card.key && 'opacity-50',
              held?.card.key === card.key && 'ring-2 ring-primary',
            ]"
            :draggable="props.canMove"
            :data-card="card.key"
            @dragstart="onDragStart($event, card)"
            @dragend="onDragEnd"
          >
            <div class="flex items-start gap-1.5">
              <NuxtLink
                :to="localeRoute({ name: 'issues-ref', params: { ref: card.key } })"
                class="min-w-0 flex-1 text-sm leading-snug hover:underline"
              >
                <span class="mr-1 font-mono text-[0.6875rem] text-muted-foreground">{{
                  card.key
                }}</span>
                {{ card.title }}
              </NuxtLink>
              <Button
                v-if="props.canMove"
                :id="`move-${card.key}`"
                variant="ghost"
                size="icon"
                class="size-6 shrink-0 cursor-grab opacity-60 group-hover:opacity-100 focus-visible:opacity-100"
                :label="$ts('projects.board.move', { issue: card.key, state: column.name })"
                aria-describedby="board-instructions"
                :aria-pressed="held?.card.key === card.key"
                @click="held?.card.key === card.key ? undefined : pickUp(card)"
                @keydown="onHeldKey"
                @blur="held?.card.key === card.key && (held = null)"
              >
                <Icon name="lucide:grip-vertical" class="size-3.5" />
              </Button>
            </div>
            <div
              v-if="card.priority || card.labels?.length || card.assignees?.length"
              class="mt-2 flex flex-wrap items-center gap-1.5"
            >
              <PriorityBadge
                v-if="card.priority"
                :priority="{ id: '', ...card.priority }"
                compact
              />
              <LabelChip
                v-for="label in card.labels"
                :key="label.name"
                :label="{ id: label.name, ...label }"
              />
              <span v-if="card.assignees?.length" class="ml-auto flex -space-x-1">
                <Avatar
                  v-for="person in card.assignees"
                  :key="person"
                  :name="person"
                  size="sm"
                  class="ring-2 ring-card"
                />
              </span>
            </div>
          </li>
          <li
            v-if="!column.cards.length"
            class="px-1 py-3 text-center text-xs text-muted-foreground"
          >
            {{ $t("projects.board.empty") }}
          </li>
        </ol>

        <Button
          v-if="column.next"
          variant="ghost"
          size="small"
          class="m-2 mt-0"
          @click="more(column)"
        >
          {{ $t("projects.board.more", { count: column.total - column.cards.length }) }}
        </Button>
      </section>
    </div>
  </div>
</template>
