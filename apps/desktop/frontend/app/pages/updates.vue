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

useHead({ title: "Updates" });

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
  { value: Interval.Daily, label: "Every day" },
  { value: Interval.Weekly, label: "Every week" },
  { value: Interval.NeverCheck, label: "Only when I ask" },
];
</script>

<template>
  <main class="mx-auto max-w-lg p-8">
    <h1 class="text-lg font-semibold">Updates</h1>

    <template v-if="state">
      <!-- Said plainly rather than hidden. A build that cannot verify a
           release will not install one, and somebody should know that before
           wondering why nothing ever updates. -->
      <section
        v-if="!state.enabled"
        class="mt-4 rounded-lg border border-warning/40 bg-warning/10 p-3"
      >
        <h2 class="text-xs font-semibold">Updates are switched off in this build</h2>
        <p class="mt-1 text-xs text-muted-foreground">
          It has no way to check that a download came from TDC, so it will not install one. You can
          still update by downloading a new version yourself.
        </p>
      </section>

      <p class="mt-2 text-sm text-muted-foreground">
        You are running version <span class="tabular">{{ state.current }}</span
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
            {{ state.checking ? "Checking…" : "Check now" }}
          </Button>

          <span v-if="state.available" class="text-sm">
            Version <span class="tabular font-medium">{{ state.available }}</span> is available.
          </span>
          <span v-else-if="!state.checking" class="text-sm text-muted-foreground">
            No newer version.
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
            Download {{ state.available }}
          </a>

          <Button v-else-if="!state.ready" :disabled="busy" @click="download"> Download it </Button>

          <template v-else>
            <Button :disabled="busy" @click="installAndSay"> Install it </Button>
            <Button variant="ghost" :disabled="busy" @click="discard"> Not now </Button>
          </template>
        </div>

        <p v-if="restartNeeded" class="mt-3 text-xs text-success-text">
          Installed. Close and reopen TDC to run the new version.
        </p>
      </section>

      <section v-if="settings" class="mt-8">
        <h2 class="text-sm font-semibold">When to look</h2>
        <p class="mt-1 text-xs text-muted-foreground">
          Checking tells the release server that TDC is installed here and which version. Nothing is
          downloaded or installed without you saying so.
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
          <span>Also check shortly after starting</span>
        </label>

        <label class="mt-2 flex items-start gap-2 text-xs">
          <input
            type="checkbox"
            class="mt-0.5"
            :checked="settings.download"
            @change="change('download', ($event.target as HTMLInputElement).checked)"
          />
          <span>
            Download an update as soon as one is found
            <span class="block text-muted-foreground">
              It is still only installed when you say so.
            </span>
          </span>
        </label>
      </section>
    </template>
  </main>
</template>
