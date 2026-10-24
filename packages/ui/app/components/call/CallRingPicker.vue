<script setup lang="ts">
import { nameOf } from "#layers/ui/app/lib/names";
import { BellRing, Globe } from "lucide-vue-next";
import { Avatar } from "#layers/ui/app/components/ui/avatar";
import { Button } from "#layers/ui/app/components/ui/button";
import { Input } from "#layers/ui/app/components/ui/input";

/**
 * Ringing people into the call: the conversation's members who are not in it.
 *
 * Somebody whose settings do not take calls from the caller is listed like
 * anybody else and simply does not ring — the list must not become a way to
 * learn who has refused whom.
 *
 * Below the list, where the server rings people on other servers, a field for
 * somebody's address. Their server answers a ring the same whether it reached
 * them or not, so "rung" here means only that the ring left: the note says so
 * rather than promising they heard it.
 */

const { $t, $ts } = useI18n();
const { user } = useAuth();
const c = useCall();
const api = useApi();
const { offers } = useServerInfo();

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

// name@server: the server end is checked by the server, which knows its peers.
const addressShape = /^[^@\s]+@[^@\s]+$/;
const address = ref("");
const ringing = ref(false);
const outcome = ref<{ address: string; sent: boolean } | null>(null);
const addressValid = computed(() => addressShape.test(address.value.trim()));

async function ringAddress() {
  const target = address.value.trim();
  if (!addressShape.test(target) || ringing.value) return;
  ringing.value = true;
  const answer = await c.ring([], [target]);
  ringing.value = false;
  const sent = answer?.rung.includes(target) ?? false;
  outcome.value = { address: target, sent };
  if (sent) address.value = "";
}
</script>

<template>
  <div class="rounded-lg border bg-muted/30 p-1.5">
    <div class="max-h-48 space-y-1 overflow-y-auto">
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

    <form
      v-if="offers('calls.federated')"
      class="mt-1.5 border-t px-1.5 pt-2 pb-0.5"
      data-testid="call-ring-address"
      @submit.prevent="ringAddress"
    >
      <div class="flex items-center gap-1.5">
        <Input
          v-model="address"
          class="h-7 text-xs"
          autocomplete="off"
          :label="$ts('calls.ringAddressLabel')"
          :placeholder="$ts('calls.personPlaceholder')"
        />
        <Button
          type="submit"
          size="small"
          variant="outline"
          class="h-7 shrink-0 px-2 text-xs"
          :disabled="!addressValid || ringing"
        >
          <Globe class="size-3" aria-hidden="true" />
          {{ $t("calls.ring") }}
        </Button>
      </div>
      <p class="mt-1 text-[0.6875rem] text-muted-foreground" role="status">
        <template v-if="outcome?.sent">
          {{ $t("calls.ringAddressSent", { address: outcome.address }) }}
        </template>
        <span v-else-if="outcome" class="text-destructive">
          {{ $t("calls.ringAddressRefused", { address: outcome.address }) }}
        </span>
        <template v-else>{{ $t("calls.ringAddressHint") }}</template>
      </p>
    </form>
  </div>
</template>
