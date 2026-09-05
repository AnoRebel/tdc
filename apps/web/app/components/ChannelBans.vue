<script setup lang="ts">
import { ShieldOff, UserX } from "lucide-vue-next";
import { Avatar } from "~/components/ui/avatar";

/**
 * Who is currently kept out of a channel, and the way back in.
 *
 * A ban that can only be issued is a ban nobody reverses. This is the other
 * half: seeing who is out, why, and until when — and lifting it.
 */

const props = defineProps<{ roomId: string }>();

const roomId = computed(() => props.roomId);
const bans = useChannelBans(roomId);

const lifting = ref<{ userId: string; username: string } | null>(null);

onMounted(() => void bans.refresh());
watch(roomId, () => void bans.refresh());

async function lift(userId: string) {
  lifting.value = null;
  try {
    await bans.lift(userId);
  } catch {
    // The list is refreshed either way, so a failure shows the ban still
    // there rather than a message about it.
    await bans.refresh();
  }
}

/** When a ban lifts, in words rather than a timestamp. */
function until(ban: { expiresAt?: string }): string {
  if (!ban.expiresAt) return "Permanent";

  const when = new Date(ban.expiresAt);
  if (when.getTime() <= Date.now()) return "Expired";

  return `Until ${when.toLocaleString([], {
    day: "numeric",
    month: "short",
    hour: "2-digit",
    minute: "2-digit",
  })}`;
}
</script>

<template>
  <section>
    <h2 class="flex items-center gap-1.5 text-sm font-semibold">
      <ShieldOff class="size-4" />
      Banned from this channel
    </h2>

    <!-- Most people cannot read this list, and the refusal is expected rather
         than exceptional: the section simply says so. -->
    <p v-if="bans.failed.value" class="mt-1 text-xs text-muted-foreground">
      You do not have permission to see who is banned here.
    </p>

    <template v-else>
      <p class="mt-1 text-xs text-muted-foreground">
        A ban applies to this channel only. Everything else they belong to is untouched.
      </p>

      <p v-if="!bans.bans.value.length" class="mt-3 text-xs text-muted-foreground">
        Nobody is banned from this channel.
      </p>

      <ul v-else class="mt-3 space-y-1">
        <li
          v-for="ban in bans.bans.value"
          :key="ban.userId"
          class="flex items-start gap-2 rounded-lg border border-border px-3 py-2"
        >
          <Avatar :name="ban.username" size="sm" class="mt-0.5" />

          <span class="min-w-0 flex-1">
            <span class="flex items-center gap-2">
              <span class="truncate text-sm font-medium">{{ ban.username }}</span>
              <span class="rounded bg-muted px-1.5 py-0.5 text-[0.6875rem] text-muted-foreground">
                {{ until(ban) }}
              </span>
            </span>
            <!-- The reason, shown rather than stored and forgotten: a decision
                 nobody can see is one nobody can review. -->
            <span class="mt-0.5 block text-xs text-muted-foreground">{{ ban.reason }}</span>
            <span class="block text-[0.6875rem] text-muted-foreground">
              by {{ ban.bannedBy.split("@")[0] }}
            </span>
          </span>

          <button
            type="button"
            class="btn btn-ghost h-7 shrink-0 px-2 text-xs"
            @click="lifting = { userId: ban.userId, username: ban.username }"
          >
            Lift ban
          </button>
        </li>
      </ul>
    </template>

    <ConfirmDialog
      v-if="lifting"
      title="Lift this ban?"
      :detail="`${lifting.username} will be able to rejoin this channel and post in it again.`"
      confirm-label="Lift ban"
      @cancel="lifting = null"
      @confirm="lift(lifting.userId)"
    />
  </section>
</template>
