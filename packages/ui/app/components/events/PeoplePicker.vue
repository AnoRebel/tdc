<script setup lang="ts">
import { nameOf } from "#layers/ui/app/lib/names";
import { Avatar } from "#layers/ui/app/components/ui/avatar";
import { Button } from "#layers/ui/app/components/ui/button";
import { Input } from "#layers/ui/app/components/ui/input";
import type { Picked } from "#layers/ui/app/lib/events";

/**
 * Choosing people on this server, by name: chips for who is chosen, and a
 * search beneath that offers matches as they are typed. Also offers channels
 * when asked to, for inviting a whole channel.
 */
const props = defineProps<{ label: string; channels?: boolean }>();
const model = defineModel<Picked[]>({ default: () => [] });

const { $t, $ts } = useI18n();
const api = useApi();
const text = ref("");
const found = ref<Picked[]>([]);
const inputId = useId();

watchDebounced(
  text,
  async (value) => {
    if (!value.trim()) {
      found.value = [];
      return;
    }
    const query = new URLSearchParams({ q: value, messages: "false", limit: "8" });
    try {
      const results = await api.get<{
        people?: { id: string; username: string; displayName?: string }[];
        rooms?: { id: string; name: string; member: boolean }[];
      }>(`/api/search/everything?${query}`);
      const chosen = new Set(model.value.map((each) => each.id));
      found.value = [
        ...(results.people ?? []).map((person) => ({
          id: person.id,
          name: nameOf(person),
          kind: "person" as const,
        })),
        ...(props.channels
          ? (results.rooms ?? [])
              .filter((room) => room.member)
              .map((room) => ({ id: room.id, name: room.name, kind: "channel" as const }))
          : []),
      ].filter((each) => !chosen.has(each.id));
    } catch {
      found.value = [];
    }
  },
  { debounce: 200 },
);

function add(each: Picked) {
  model.value = [...model.value, each];
  text.value = "";
  found.value = [];
}

function remove(id: string) {
  model.value = model.value.filter((each) => each.id !== id);
}
</script>

<template>
  <div>
    <label :for="inputId" class="text-xs font-medium">{{ props.label }}</label>
    <ul v-if="model.length" class="mt-1.5 flex flex-wrap gap-1.5">
      <li
        v-for="each in model"
        :key="each.id"
        class="flex items-center gap-1 rounded-full border bg-surface py-0.5 pr-0.5 pl-1 text-xs"
      >
        <Avatar v-if="each.kind === 'person'" :name="each.name" size="sm" />
        <Icon v-else name="lucide:hash" class="size-3 text-muted-foreground" aria-hidden="true" />
        <span>{{ each.name }}</span>
        <Button
          variant="ghost"
          size="icon"
          class="size-5"
          :label="$ts('events.form.removePerson', { name: each.name })"
          @click="remove(each.id)"
        >
          <Icon name="lucide:x" class="size-3" aria-hidden="true" />
        </Button>
      </li>
    </ul>
    <Input
      :id="inputId"
      v-model="text"
      class="mt-1.5"
      :placeholder="
        props.channels ? $ts('events.form.searchPeopleOrChannels') : $ts('events.form.searchPeople')
      "
      autocomplete="off"
    />
    <ul
      v-if="found.length"
      class="mt-1 max-h-40 overflow-y-auto rounded-lg border bg-popover p-1 shadow-md"
    >
      <li v-for="each in found" :key="each.id">
        <button
          type="button"
          class="flex w-full cursor-pointer items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm hover:bg-accent"
          @click="add(each)"
        >
          <Avatar v-if="each.kind === 'person'" :name="each.name" size="sm" />
          <Icon
            v-else
            name="lucide:hash"
            class="size-3.5 text-muted-foreground"
            aria-hidden="true"
          />
          {{ each.name }}
          <span
            v-if="each.kind === 'channel'"
            class="ml-auto text-[0.6875rem] text-muted-foreground"
          >
            {{ $t("events.form.wholeChannel") }}
          </span>
        </button>
      </li>
    </ul>
  </div>
</template>
