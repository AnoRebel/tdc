<script setup lang="ts" generic="TData extends Record<string, unknown>">
import {
  createPaginatedRowModel,
  createSortedRowModel,
  FlexRender,
  rowPaginationFeature,
  rowSortingFeature,
  sortFn_alphanumeric,
  tableFeatures,
  useTable,
} from "@tanstack/vue-table";

import { Button } from "#layers/ui/app/components/ui/button";

/**
 * A table of things, sortable and paginated.
 *
 * The columns are defined by the caller rather than the markup, which is the
 * point: sorting and paging are the same problem on every screen that shows a
 * list, and solving it once per page is how they end up behaving differently
 * from each other.
 *
 * Vendored rather than depended on. shadcn-vue is a set of components meant to
 * be copied and owned — the alternative is a dependency whose next release
 * decides what this application's tables look like.
 */

/*
 * The capabilities this table has, declared rather than assumed.
 *
 * Version 9 asks for them explicitly, which is the better default: a list of
 * four rows pays nothing for the paging it never uses, and the row models are
 * only built for what was asked for.
 */
const features = tableFeatures({
  rowSortingFeature,
  rowPaginationFeature,
  sortedRowModel: createSortedRowModel(),
  paginatedRowModel: createPaginatedRowModel(),
  sortFns: { alphanumeric: sortFn_alphanumeric },
});

const props = withDefaults(
  defineProps<{
    // eslint-disable-next-line @typescript-eslint/no-explicit-any -- a column
    // describes how to render one field of TData, and narrowing it here would
    // force every caller to restate what the column definition already says.
    columns: any[];
    data: TData[];
    /** How many rows a page holds. */
    pageSize?: number;
    /** What to say when there is nothing, which is not the same as loading. */
    empty?: string;
    /** A label for the table itself, since a screen reader announces it. */
    caption?: string;
  }>(),
  { pageSize: 25, empty: "Nothing here yet.", caption: undefined },
);

const table = useTable({
  features,
  get data() {
    return props.data;
  },
  get columns() {
    return props.columns;
  },
  initialState: { pagination: { pageIndex: 0, pageSize: props.pageSize } },
});

const rows = computed(() => table.getRowModel().rows);
const pageCount = computed(() => table.getPageCount());
/*
 * Read through the atom rather than a getState() call, which version 9 no
 * longer offers: state lives in atoms so a component can subscribe to the one
 * slice it needs instead of re-rendering on every change to anything.
 */
const pageIndex = computed(() => table.atoms.pagination.get().pageIndex);
</script>

<template>
  <div class="overflow-hidden rounded-lg border">
    <!-- Its own scroller, so a wide table scrolls inside the page rather than
         making the whole page scroll sideways. -->
    <div class="overflow-x-auto">
      <table class="w-full text-sm">
        <caption v-if="caption" class="sr-only">
          {{
            caption
          }}
        </caption>

        <thead>
          <tr
            v-for="headerGroup in table.getHeaderGroups()"
            :key="headerGroup.id"
            class="border-b text-left"
          >
            <th
              v-for="header in headerGroup.headers"
              :key="header.id"
              scope="col"
              class="px-3 py-2 text-xs font-medium text-muted-foreground"
              :aria-sort="
                header.column.getIsSorted() === 'asc'
                  ? 'ascending'
                  : header.column.getIsSorted() === 'desc'
                    ? 'descending'
                    : header.column.getCanSort()
                      ? 'none'
                      : undefined
              "
            >
              <!-- A sortable column is a button, not a heading somebody has to
                   guess is clickable — and a button is reachable by keyboard
                   where a click handler on a th is not. -->
              <button
                v-if="header.column.getCanSort()"
                type="button"
                class="flex items-center gap-1 hover:text-foreground"
                @click="header.column.toggleSorting()"
              >
                <FlexRender :render="header.column.columnDef.header" :props="header.getContext()" />
                <Icon
                  v-if="header.column.getIsSorted()"
                  :name="
                    header.column.getIsSorted() === 'asc'
                      ? 'lucide:chevron-up'
                      : 'lucide:chevron-down'
                  "
                  class="size-3"
                />
              </button>

              <FlexRender
                v-else
                :render="header.column.columnDef.header"
                :props="header.getContext()"
              />
            </th>
          </tr>
        </thead>

        <tbody>
          <tr v-for="row in rows" :key="row.id" class="border-b last:border-0 hover:bg-muted/40">
            <!-- Every cell, since this table does not offer hiding columns:
                 getVisibleCells belongs to a feature that is not enabled, and
                 asking for it would mean shipping that feature to every table
                 for a capability none of them expose. -->
            <td v-for="cell in row.getAllCells()" :key="cell.id" class="px-3 py-2">
              <FlexRender :render="cell.column.columnDef.cell" :props="cell.getContext()" />
            </td>
          </tr>

          <tr v-if="!rows.length">
            <td
              :colspan="columns.length"
              class="px-3 py-8 text-center text-sm text-muted-foreground"
            >
              {{ empty }}
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- Only where there is more than one page. Paging controls on a single
         page are furniture that does nothing. -->
    <div
      v-if="pageCount > 1"
      class="flex items-center justify-between border-t px-3 py-2 text-xs text-muted-foreground"
    >
      <span class="tabular-nums">Page {{ pageIndex + 1 }} of {{ pageCount }}</span>

      <div class="flex gap-1">
        <Button
          variant="ghost"
          class="h-7 px-2"
          :disabled="!table.getCanPreviousPage()"
          @click="table.previousPage()"
        >
          Previous
        </Button>
        <Button
          variant="ghost"
          class="h-7 px-2"
          :disabled="!table.getCanNextPage()"
          @click="table.nextPage()"
        >
          Next
        </Button>
      </div>
    </div>
  </div>
</template>
