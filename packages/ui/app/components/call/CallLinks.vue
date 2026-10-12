<script setup lang="ts">
import type { CallLink } from "@tdc/types";
import { isApiError } from "@tdc/types";
import { Copy, Link2, Trash2 } from "lucide-vue-next";
import { Button } from "#layers/ui/app/components/ui/button";
import { Input } from "#layers/ui/app/components/ui/input";
import { Switch } from "#layers/ui/app/components/ui/switch";
import { onDayAtTime } from "#layers/ui/app/lib/dates";

/**
 * Links to this conversation's call: making one, copying it, and withdrawing
 * the ones already made.
 *
 * A link lets whoever holds it into the call and nothing else. It can ask for
 * a passcode, hold people in a lobby until somebody here lets them in, expire,
 * and stop after a number of uses. Its address is shown once, when it is made;
 * the server keeps only a hash of it.
 */

const { $t, $ts } = useI18n();
const api = useApi();
const c = useCall();
const serverBase = useMediaBase();

const links = ref<CallLink[]>([]);
const made = ref<string | null>(null);
const copied = ref(false);
const failure = ref<string | null>(null);
const busy = ref(false);

const label = ref("");
const protect = ref(false);
const passcode = ref("");
const lobby = ref(true);
const expiry = ref("24");
const maxUses = ref("");

const room = computed(() => c.roomId.value);

async function load() {
  if (!room.value) return;
  try {
    links.value = await api.get<CallLink[]>(`/api/rooms/${room.value}/call/links`);
  } catch {
    links.value = [];
  }
}
onMounted(load);

/** The address a link is opened at: this site, or the server in the desktop app. */
function addressOf(secret: string) {
  const base = serverBase || window.location.origin;
  return `${base}/call/${secret}`;
}

async function make() {
  if (!room.value) return;
  busy.value = true;
  failure.value = null;
  try {
    const link = await api.post<CallLink>(`/api/rooms/${room.value}/call/links`, {
      label: label.value,
      passcode: protect.value ? passcode.value : "",
      lobby: lobby.value,
      expiresInHours: Number(expiry.value) || 0,
      maxUses: Number(maxUses.value) || 0,
    });
    made.value = link.secret ? addressOf(link.secret) : null;
    copied.value = false;
    label.value = "";
    passcode.value = "";
    await load();
  } catch (cause) {
    failure.value = isApiError(cause) ? cause.message : $ts("calls.links.couldNotMake");
  } finally {
    busy.value = false;
  }
}

async function copy() {
  if (!made.value) return;
  await navigator.clipboard?.writeText(made.value).catch(() => {});
  copied.value = true;
}

async function revoke(link: CallLink) {
  if (!room.value) return;
  const before = links.value;
  links.value = links.value.filter((each) => each.id !== link.id);
  try {
    await api.delete(`/api/rooms/${room.value}/call/links/${link.id}`);
  } catch {
    links.value = before;
  }
}
</script>

<template>
  <div class="max-h-80 space-y-2 overflow-y-auto rounded-lg border bg-muted/30 p-2 text-xs">
    <form class="space-y-2" @submit.prevent="make">
      <Input
        v-model="label"
        :label="$ts('calls.links.label')"
        :placeholder="$ts('calls.links.label')"
      />
      <label class="flex items-center justify-between gap-2">
        <span>{{ $t("calls.links.lobby") }}</span>
        <Switch v-model="lobby" :label="$ts('calls.links.lobby')" />
      </label>
      <label class="flex items-center justify-between gap-2">
        <span>{{ $t("calls.links.protect") }}</span>
        <Switch v-model="protect" :label="$ts('calls.links.protect')" />
      </label>
      <Input
        v-if="protect"
        v-model="passcode"
        :label="$ts('calls.links.passcode')"
        :placeholder="$ts('calls.links.passcode')"
        autocomplete="off"
      />
      <div class="flex gap-2">
        <label class="flex flex-1 flex-col gap-1">
          <span class="text-muted-foreground">{{ $t("calls.links.expires") }}</span>
          <select v-model="expiry" class="h-8 rounded-md border bg-background px-1">
            <option value="1">{{ $t("calls.links.hour") }}</option>
            <option value="24">{{ $t("calls.links.day") }}</option>
            <option value="168">{{ $t("calls.links.week") }}</option>
            <option value="0">{{ $t("calls.links.never") }}</option>
          </select>
        </label>
        <label class="flex flex-1 flex-col gap-1">
          <span class="text-muted-foreground">{{ $t("calls.links.uses") }}</span>
          <input
            v-model="maxUses"
            type="number"
            min="0"
            inputmode="numeric"
            class="h-8 rounded-md border bg-background px-2"
            :placeholder="$ts('calls.links.unlimited')"
          />
        </label>
      </div>
      <p v-if="failure" class="text-destructive" role="alert">{{ failure }}</p>
      <Button
        type="submit"
        size="small"
        class="w-full"
        :disabled="busy || (protect && passcode.length < 4)"
      >
        <Link2 class="size-3.5" aria-hidden="true" />
        {{ $t("calls.links.make") }}
      </Button>
    </form>

    <div
      v-if="made"
      class="space-y-1 rounded-md border border-primary/30 bg-background p-2"
      role="status"
    >
      <p class="font-medium">{{ $t("calls.links.madeOnce") }}</p>
      <div class="flex items-center gap-1">
        <code class="min-w-0 flex-1 truncate rounded bg-muted px-1 py-0.5">{{ made }}</code>
        <Button
          variant="ghost"
          size="icon"
          class="size-7"
          :label="$ts('calls.links.copy')"
          @click="copy"
        >
          <Copy class="size-3.5" />
        </Button>
      </div>
      <p v-if="copied" class="text-muted-foreground">{{ $t("calls.links.copied") }}</p>
    </div>

    <ul
      v-if="links.length"
      class="space-y-1 border-t pt-2"
      :aria-label="$ts('calls.links.existing')"
    >
      <li v-for="link in links" :key="link.id" class="flex items-center gap-2">
        <span class="min-w-0 flex-1 truncate">
          <span class="font-medium">{{ link.label || $t("calls.links.untitled") }}</span>
          <span class="text-muted-foreground">
            · {{ $t("calls.links.used", { count: link.uses }) }}
            <template v-if="link.protected"> · {{ $t("calls.links.protected") }}</template>
            <template v-if="link.lobby"> · {{ $t("calls.links.withLobby") }}</template>
            <template v-if="link.expiresAt">
              · {{ $t("calls.links.until", { when: onDayAtTime(link.expiresAt) }) }}</template
            >
          </span>
        </span>
        <Button
          variant="ghost"
          size="icon"
          class="size-6"
          :label="$ts('calls.links.revoke', { name: link.label || $ts('calls.links.untitled') })"
          @click="revoke(link)"
        >
          <Trash2 class="size-3 text-destructive" />
        </Button>
      </li>
    </ul>
  </div>
</template>
