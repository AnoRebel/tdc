<script setup lang="ts">
/**
 * Whether there is a newer version, and when to look.
 *
 * Nothing on this screen happens on its own. Checking says what exists,
 * downloading prepares it, and installing replaces the application — which is
 * somebody's decision, so it is a button rather than a consequence.
 */
import { Interval } from "#bindings/updater/models";
import { Button } from "#layers/ui/app/components/ui/button";
import { ArrowLeft } from "lucide-vue-next";

const { $t, $ts } = useI18n();

const router = useRouter();

useHead({ title: $ts("desktop.settings.updates") });

/*
 * Where somebody downloads a version by hand.
 *
 * Needed whenever this build cannot install one for them, which is every build
 * until there is a signed release — so it is not an edge case to tuck away.
 */
const releasesURL = "https://github.com/AnoRebel/tdc/releases";

const { state, settings, busy, refresh, check, download, install, discard, save } = useUpdates();

const restartNeeded = ref(false);

onMounted(() => void refresh());

async function installAndSay() {
  await install();
  // The replacement is on disk; what is running is still the old version in
  // memory. Saying so is more honest than a progress bar that finishes into
  // nothing visible.
  restartNeeded.value = true;
}

/** Saves one field without disturbing the others. */
async function change(field: "onLaunch" | "download", value: boolean) {
  if (!settings.value) return;
  await save({ ...settings.value, [field]: value });
}

async function changeInterval(every: Interval) {
  if (!settings.value) return;
  await save({ ...settings.value, every });
}

/*
 * The intervals as the Go side defines them, rather than as strings written
 * again here — a value this side invented would be refused on arrival.
 */
const intervals = [
  { value: Interval.Daily, label: $ts("desktop.updates.everyDay") },
  { value: Interval.Weekly, label: $ts("desktop.updates.everyWeek") },
  { value: Interval.NeverCheck, label: $ts("desktop.updates.onlyWhenAsked") },
];
</script>

<template>
  <main class="mx-auto max-w-lg p-8">
    <!-- Back to wherever this was opened from: the menu or settings. -->
    <button type="button" class="btn btn-ghost mb-4 -ml-2 h-7 px-2 text-xs" @click="router.back()">
      <ArrowLeft class="size-3.5" />
      {{ $t("action.back") }}
    </button>
    <h1 class="text-lg font-semibold">{{ $t("desktop.settings.updates") }}</h1>

    <template v-if="state">
      <!-- Said plainly rather than hidden. A build that cannot verify a
           release will not install one, and somebody should know that before
           wondering why nothing ever updates. -->
      <section
        v-if="!state.enabled"
        class="mt-4 rounded-lg border border-warning/40 bg-warning/10 p-3"
      >
        <h2 class="text-xs font-semibold">{{ $t("desktop.updates.switchedOff") }}</h2>
        <p class="mt-1 text-xs text-muted-foreground">
          {{ $t("desktop.updates.switchedOffHint") }}
        </p>
      </section>

      <p class="mt-2 text-sm text-muted-foreground">
        {{ $t("desktop.updates.runningVersion") }}
        <span class="tabular">{{ state.current }}</span
        >.
      </p>

      <!--
        Checking works whether or not this build can install what it finds.

        Knowing a newer version exists is useful on a machine that cannot
        verify one — it is the difference between updating by hand and not
        knowing there is anything to update to.
      -->
      <section class="mt-5">
        <div class="flex flex-wrap items-center gap-2">
          <Button variant="outline" :disabled="busy" @click="check">
            {{ state.checking ? $t("desktop.checking") : $t("desktop.updates.checkNow") }}
          </Button>

          <span v-if="state.available" class="text-sm">
            {{ $t("desktop.updates.version") }}
            <span class="tabular font-medium">{{ state.available }}</span>
            {{ $t("desktop.updates.isAvailable") }}
          </span>
          <span v-else-if="!state.checking" class="text-sm text-muted-foreground">
            {{ $t("desktop.updates.noNewer") }}
          </span>
        </div>

        <p v-if="state.notes" class="mt-2 text-xs whitespace-pre-line text-muted-foreground">
          {{ state.notes }}
        </p>

        <!-- The server's own words about what went wrong. It knows and this
             does not, and the reason is usually actionable. -->
        <p v-if="state.problem" role="alert" class="mt-2 text-xs text-destructive">
          {{ state.problem }}
        </p>

        <div v-if="state.available" class="mt-3 flex flex-wrap gap-2">
          <!-- Where this build cannot install, the only thing offered is the
               page somebody downloads from. -->
          <a
            v-if="!state.enabled"
            class="btn btn-primary"
            :href="releasesURL"
            target="_blank"
            rel="noreferrer"
          >
            {{ $t("desktop.updates.downloadVersion", { version: state.available }) }}
          </a>

          <Button v-else-if="!state.ready" :disabled="busy" @click="download">
            {{ $t("desktop.updates.downloadIt") }}
          </Button>

          <template v-else>
            <Button :disabled="busy" @click="installAndSay">
              {{ $t("desktop.updates.installIt") }}
            </Button>
            <Button variant="ghost" :disabled="busy" @click="discard">
              {{ $t("update.notNow") }}
            </Button>
          </template>
        </div>

        <p v-if="restartNeeded" class="mt-3 text-xs text-success-text">
          {{ $t("desktop.updates.installed") }}
        </p>
      </section>

      <section v-if="settings" class="mt-8">
        <h2 class="text-sm font-semibold">{{ $t("desktop.updates.whenToLook") }}</h2>
        <p class="mt-1 text-xs text-muted-foreground">
          {{ $t("desktop.updates.whenToLookHint") }}
        </p>

        <div class="mt-3 flex flex-wrap gap-1.5">
          <Button
            v-for="option in intervals"
            :key="option.value"
            :variant="settings.every === option.value ? 'primary' : 'outline'"
            :aria-pressed="settings.every === option.value"
            @click="changeInterval(option.value)"
          >
            {{ option.label }}
          </Button>
        </div>

        <label class="mt-4 flex items-start gap-2 text-xs">
          <input
            type="checkbox"
            class="mt-0.5"
            :checked="settings.onLaunch"
            @change="change('onLaunch', ($event.target as HTMLInputElement).checked)"
          />
          <span>{{ $t("desktop.updates.checkOnLaunch") }}</span>
        </label>

        <label class="mt-2 flex items-start gap-2 text-xs">
          <input
            type="checkbox"
            class="mt-0.5"
            :checked="settings.download"
            @change="change('download', ($event.target as HTMLInputElement).checked)"
          />
          <span>
            {{ $t("desktop.updates.autoDownload") }}
            <span class="block text-muted-foreground">
              {{ $t("desktop.updates.autoDownloadHint") }}
            </span>
          </span>
        </label>
      </section>
    </template>
  </main>
</template>
