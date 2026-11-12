<script setup lang="ts">
import { Play, Trash2, Upload } from "lucide-vue-next";
import { Button } from "#layers/ui/app/components/ui/button";
import { Input } from "#layers/ui/app/components/ui/input";

/**
 * How calls ring on this device: on or off, the default tone, tones somebody
 * has added, and a tone for particular people. Kept on this device and for this
 * server only — see useRingtones.
 */

const { $t, $ts } = useI18n();
const r = useRingtones();
// Offered only where the device can vibrate, so a laptop does not show a
// setting that can do nothing.
const vibrates = canVibrate();
const problem = ref<string | null>(null);
const newPerson = ref("");
const newPersonTone = ref("classic");

let stopPreview: (() => void) | null = null;
function preview(id: string) {
  stopPreview?.();
  stopPreview = r.play(id, false);
  setTimeout(() => stopPreview?.(), 3000);
}
onBeforeUnmount(() => stopPreview?.());

async function add(event: Event) {
  const file = (event.target as HTMLInputElement).files?.[0];
  (event.target as HTMLInputElement).value = "";
  if (!file) return;
  const outcome = await r.addTone(file);
  problem.value =
    outcome === "too-large"
      ? $ts("calls.toneTooLarge")
      : outcome === "not-audio"
        ? $ts("calls.toneNotAudio")
        : null;
}

function addPerson() {
  const address = newPerson.value.trim();
  if (!address) return;
  r.setPersonTone(address, newPersonTone.value);
  newPerson.value = "";
}

const people = computed(() => Object.entries(r.settings.value.byPerson));
const toneName = (id: string) => r.tones.value.find((tone) => tone.id === id)?.name ?? id;
</script>

<template>
  <div class="space-y-3" data-ringtones>
    <label class="flex cursor-pointer items-center gap-2 text-sm">
      <input
        type="checkbox"
        class="accent-primary"
        :checked="!r.settings.value.muted"
        @change="r.setMuted(!r.settings.value.muted)"
      />
      {{ $t("calls.ringerOn") }}
    </label>
    <label v-if="vibrates" class="flex cursor-pointer items-center gap-2 text-sm">
      <input
        type="checkbox"
        class="accent-primary"
        :checked="r.settings.value.vibrate"
        @change="r.setVibrate(!r.settings.value.vibrate)"
      />
      {{ $t("calls.vibrate") }}
    </label>

    <div class="flex flex-wrap items-center gap-2">
      <span class="text-xs font-medium">{{ $t("calls.defaultTone") }}</span>
      <select
        class="h-8 rounded-md border bg-background px-2 text-sm"
        :value="r.settings.value.tone"
        :aria-label="$ts('calls.defaultTone')"
        @change="r.setDefault(($event.target as HTMLSelectElement).value)"
      >
        <option v-for="tone in r.tones.value" :key="tone.id" :value="tone.id">
          {{ tone.name }}
        </option>
      </select>
      <Button
        variant="ghost"
        size="icon"
        class="size-8"
        :label="$ts('calls.previewTone')"
        @click="preview(r.settings.value.tone)"
      >
        <Play class="size-3.5" />
      </Button>
      <label class="btn btn-ghost h-8 cursor-pointer px-2 text-xs">
        <Upload class="size-3.5" aria-hidden="true" />
        {{ $t("calls.addTone") }}
        <input type="file" accept="audio/*" class="sr-only" @change="add" />
      </label>
    </div>
    <p v-if="problem" class="text-xs text-destructive" role="alert">{{ problem }}</p>

    <ul v-if="r.settings.value.custom.length" class="space-y-1">
      <li
        v-for="tone in r.settings.value.custom"
        :key="tone.id"
        class="flex items-center gap-2 text-sm"
      >
        <span class="min-w-0 flex-1 truncate">{{ tone.name }}</span>
        <Button
          variant="ghost"
          size="icon"
          class="size-7"
          :label="$ts('calls.previewTone')"
          @click="preview(tone.id)"
        >
          <Play class="size-3.5" />
        </Button>
        <Button
          variant="ghost"
          size="icon"
          class="size-7"
          :label="$ts('calls.removeTone')"
          @click="r.removeTone(tone.id)"
        >
          <Trash2 class="size-3.5" />
        </Button>
      </li>
    </ul>

    <div>
      <p class="text-xs font-medium">{{ $t("calls.personTones") }}</p>
      <ul class="mt-1 space-y-1">
        <li
          v-for="[address, tone] in people"
          :key="address"
          class="flex items-center gap-2 text-sm"
        >
          <span class="min-w-0 flex-1 truncate">{{ address }}</span>
          <span class="text-xs text-muted-foreground">{{ toneName(tone) }}</span>
          <Button
            variant="ghost"
            size="icon"
            class="size-7"
            :label="$ts('calls.removeTone')"
            @click="r.setPersonTone(address, null)"
          >
            <Trash2 class="size-3.5" />
          </Button>
        </li>
      </ul>
      <form class="mt-2 flex flex-wrap items-center gap-2" @submit.prevent="addPerson">
        <Input v-model="newPerson" class="h-8 w-56" :placeholder="$ts('calls.personPlaceholder')" />
        <select
          v-model="newPersonTone"
          class="h-8 rounded-md border bg-background px-2 text-sm"
          :aria-label="$ts('calls.defaultTone')"
        >
          <option v-for="tone in r.tones.value" :key="tone.id" :value="tone.id">
            {{ tone.name }}
          </option>
        </select>
        <Button size="small" type="submit">{{ $t("calls.setPersonTone") }}</Button>
      </form>
    </div>
  </div>
</template>
