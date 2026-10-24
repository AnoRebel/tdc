<script setup lang="ts">
import { nameOf } from "#layers/ui/app/lib/names";
import { BellRing } from "lucide-vue-next";
import { Avatar } from "#layers/ui/app/components/ui/avatar";
import { Button } from "#layers/ui/app/components/ui/button";

/**
 * Ringing people into the call: the conversation's members who are not in it.
 *
 * Somebody whose settings do not take calls from the caller is listed like
 * anybody else and simply does not ring — the list must not become a way to
 * learn who has refused whom.
 */

const { $t, $ts } = useI18n();
const { user } = useAuth();
const c = useCall();
const api = useApi();

const members = ref<{ id: string; username: string; displayName?: string }[]>([]);
const rung = ref(new Set<string>());

onMounted(async () => {
  if (!c.roomId.value) return;
  try {
    members.value = await api.get(`/api/rooms/${c.roomId.value}/members`);
  } catch {
    members.value = [];
  }
});

const ringable = computed(() => {
  const present = new Set(c.call.value?.participants.map((each) => each.userId) ?? []);
  return members.value.filter((each) => each.id !== user.value?.id && !present.has(each.id));
});

async function ringOne(id: string) {
  await c.ring([id]);
  rung.value = new Set([...rung.value, id]);
}
</script>

<template>
  <div class="max-h-48 space-y-1 overflow-y-auto rounded-lg border bg-muted/30 p-1.5">
    <p v-if="!ringable.length" class="px-1.5 py-1 text-xs text-muted-foreground">
      {{ $t("calls.nobodyToRing") }}
    </p>
    <div
      v-for="member in ringable"
      :key="member.id"
      class="flex items-center gap-2 rounded px-1.5 py-1 hover:bg-muted"
    >
      <Avatar :name="nameOf(member)" size="sm" />
      <span class="min-w-0 flex-1 truncate text-xs">{{ nameOf(member) }}</span>
      <Button
        size="small"
        variant="ghost"
        class="h-6 px-2 text-xs"
        :disabled="rung.has(member.id)"
        :label="$ts('calls.ringPerson', { name: nameOf(member) })"
        @click="ringOne(member.id)"
      >
        <BellRing class="size-3" aria-hidden="true" />
        {{ rung.has(member.id) ? $t("calls.rung") : $t("calls.ring") }}
      </Button>
    </div>
  </div>
</template>
