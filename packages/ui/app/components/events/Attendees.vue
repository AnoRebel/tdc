<script setup lang="ts">
import type { EventAttendance, EventDetail } from "@tdc/types";
import { Avatar } from "#layers/ui/app/components/ui/avatar";

/**
 * Who is going. People who keep the events they go to to themselves are
 * counted, not named — the server never sends their names to anybody but the
 * event's organisers.
 */
const props = defineProps<{ event: EventDetail }>();
const { $t } = useI18n();
const eventsApi = useEvents();

const attendance = ref<EventAttendance | null>(null);
async function refresh() {
  try {
    attendance.value = await eventsApi.attendees(props.event.id);
  } catch {
    attendance.value = null;
  }
}
watch(
  () => props.event.id,
  () => void refresh(),
  { immediate: true },
);
defineExpose({ refresh });

const groups = computed(() => {
  const listed = attendance.value?.attendees ?? [];
  return (["going", "maybe", "waitlisted", "declined"] as const)
    .map((status) => ({
      status,
      people: listed.filter((person) => person.status === status),
      unnamed: attendance.value?.unnamed?.[status] ?? 0,
    }))
    .filter((group) => group.people.length || group.unnamed);
});
</script>

<template>
  <section v-if="groups.length" aria-labelledby="attendees-heading">
    <h2 id="attendees-heading" class="text-sm font-semibold">{{ $t("events.attendees") }}</h2>
    <div class="mt-2 space-y-3">
      <div v-for="group in groups" :key="group.status">
        <h3 class="text-xs text-muted-foreground">
          {{ $t(`events.answers.${group.status}`) }} · {{ group.people.length + group.unnamed }}
        </h3>
        <ul class="mt-1.5 flex flex-wrap gap-1.5">
          <li
            v-for="person in group.people"
            :key="person.userId"
            class="flex items-center gap-1.5 rounded-full border bg-surface py-0.5 pr-2.5 pl-0.5 text-xs"
          >
            <Avatar :name="person.username" size="sm" />{{ person.displayName || person.username }}
          </li>
          <li
            v-if="group.unnamed"
            class="rounded-full bg-muted px-2.5 py-1 text-xs text-muted-foreground"
          >
            {{ $t("events.unnamed", { count: group.unnamed }) }}
          </li>
        </ul>
      </div>
    </div>
  </section>
</template>
