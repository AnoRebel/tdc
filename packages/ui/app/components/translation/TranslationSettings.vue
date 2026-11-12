<script setup lang="ts">
import { Check, Languages } from "lucide-vue-next";

import { isApiError, type TranslationProvider, type TranslationProviderState } from "@tdc/types";
import { Button } from "#layers/ui/app/components/ui/button";
import { Select } from "#layers/ui/app/components/ui/select";
import { Switch } from "#layers/ui/app/components/ui/switch";
import TranslationConnect from "#layers/ui/app/components/translation/TranslationConnect.vue";

/**
 * Translation: which provider, into what, and when.
 *
 * Each provider says, in one sentence, who sees the text sent to it — the
 * thing somebody most needs to know before choosing one, and the thing a
 * settings screen most often leaves out. Loaded only when settings are opened,
 * so nobody pays for this screen on the way to a conversation.
 */

const { $t, $ts, $getLocale } = useI18n();
const translation = useTranslation();

const error = ref<string | null>(null);
const busy = ref(false);
/** The provider whose connection form is open, if any. */
const editing = ref<TranslationProvider | null>(null);
/** The provider about to be disconnected, held until confirmed. */
const removing = ref<TranslationProvider | null>(null);

onMounted(() => void translation.load());

const overview = translation.overview;
const preferences = computed(() => overview.value?.preferences);

/*
 * Languages offered as targets. A fixed list of widely spoken ones and the
 * ones this community writes in; each named in the reader's own language by
 * the browser, so the list needs no translating of its own.
 */
const LANGUAGES = [
  "sw",
  "en",
  "fr",
  "ar",
  "pt",
  "es",
  "de",
  "it",
  "nl",
  "ru",
  "tr",
  "pl",
  "uk",
  "zh",
  "ja",
  "ko",
  "hi",
  "id",
  "vi",
  "am",
  "so",
  "rw",
  "yo",
  "ha",
  "zu",
];

function languageName(code: string): string {
  try {
    return new Intl.DisplayNames([$getLocale()], { type: "language" }).of(code) ?? code;
  } catch {
    return code;
  }
}

/* Selects cannot hold an empty value, so "the default" is spelled out. */
const DEFAULT = "auto";

const languageOptions = computed(() => [
  {
    value: DEFAULT,
    label: $ts("translation.languageDefault", { language: languageName($getLocale()) }),
  },
  ...LANGUAGES.map((code) => ({ value: code, label: languageName(code) })).sort((a, b) =>
    a.label.localeCompare(b.label),
  ),
]);

const providerLabels = computed<Record<TranslationProvider, string>>(() => ({
  device: $ts("translation.provider.device"),
  libretranslate: "LibreTranslate",
  google: "Google Cloud Translation",
  microsoft: "Microsoft Translator",
  deepl: "DeepL",
}));

const providerOptions = computed(() => [
  {
    value: DEFAULT,
    label: translation.automaticProvider.value
      ? $ts("translation.automaticIs", {
          provider: providerLabels.value[translation.automaticProvider.value],
        })
      : $ts("translation.automatic"),
  },
  ...(overview.value?.providers ?? [])
    .filter((provider) => translation.usable(provider.name))
    .map((provider) => ({ value: provider.name, label: providerLabels.value[provider.name] })),
]);

async function save(
  change: Partial<{ provider: string; targetLanguage: string; autoTranslate: boolean }>,
) {
  if (!preferences.value) return;
  error.value = null;
  busy.value = true;
  try {
    const next = { ...preferences.value, ...change };
    await translation.setPreferences({
      provider: (next.provider === DEFAULT ? "" : next.provider) as TranslationProvider | "",
      targetLanguage: next.targetLanguage === DEFAULT ? "" : next.targetLanguage,
      autoTranslate: next.autoTranslate,
    });
  } catch (cause) {
    error.value = isApiError(cause) ? cause.message : $ts("translation.couldNotSave");
  } finally {
    busy.value = false;
  }
}

const provider = computed({
  get: () => preferences.value?.provider || DEFAULT,
  set: (value: string) => void save({ provider: value }),
});
const language = computed({
  get: () => preferences.value?.targetLanguage || DEFAULT,
  set: (value: string) => void save({ targetLanguage: value }),
});
const auto = computed({
  get: () => preferences.value?.autoTranslate ?? false,
  set: (value: boolean) => void save({ autoTranslate: value }),
});

/** Who sees a message sent to this provider, in one honest sentence. */
function whoSees(name: TranslationProvider): string {
  return $ts(`translation.privacy.${name}`);
}

/*
 * Whether this person connected the provider themselves. LibreTranslate also
 * counts as connected through the operator's instance, which is not theirs to
 * change or remove; their own instance is the one with an address saved.
 */
function own(state: TranslationProviderState): boolean {
  return state.name === "libretranslate" ? Boolean(state.endpoint) : state.connected;
}

function status(state: TranslationProviderState): string {
  if (!state.allowed) return $ts("translation.notAllowed");
  if (state.name === "device") {
    return translation.deviceSupported()
      ? $ts("translation.deviceReady")
      : $ts("translation.deviceUnsupported");
  }
  if (own(state)) return state.hasKey ? $ts("translation.keySaved") : $ts("translation.connected");
  if (state.name === "libretranslate" && state.shared) return $ts("translation.sharedInstance");
  return $ts("translation.notConnected");
}

async function confirmRemove() {
  const name = removing.value;
  removing.value = null;
  if (!name) return;
  error.value = null;
  try {
    await translation.disconnect(name);
  } catch (cause) {
    error.value = isApiError(cause) ? cause.message : $ts("translation.couldNotRemove");
  }
}
</script>

<template>
  <section v-if="translation.offered.value" id="translation" class="scroll-mt-6">
    <h2 class="flex items-center gap-1.5 text-sm font-semibold">
      <Languages class="size-4" />
      {{ $t("translation.title") }}
    </h2>
    <p class="mt-1 text-xs text-muted-foreground">{{ $t("translation.hint") }}</p>

    <p
      v-if="error"
      class="mt-2 rounded-md bg-destructive/10 px-3 py-2 text-xs text-destructive"
      role="alert"
    >
      {{ error }}
    </p>

    <template v-if="overview && preferences">
      <div class="mt-3 grid gap-3 sm:grid-cols-2">
        <label class="block text-xs font-medium">
          {{ $t("translation.translateInto") }}
          <Select
            v-model="language"
            class="mt-1"
            :label="$ts('translation.translateInto')"
            :options="languageOptions"
            :disabled="busy"
          />
        </label>
        <label class="block text-xs font-medium">
          {{ $t("translation.provider.label") }}
          <Select
            v-model="provider"
            class="mt-1"
            :label="$ts('translation.provider.label')"
            :options="providerOptions"
            :disabled="busy"
          />
        </label>
      </div>

      <label class="mt-3 flex items-start justify-between gap-3">
        <span class="text-sm">
          {{ $t("translation.autoServer") }}
          <span class="mt-0.5 block text-xs text-muted-foreground">
            {{ $t("translation.autoServerHint") }}
          </span>
        </span>
        <Switch v-model="auto" :label="$ts('translation.autoServer')" :disabled="busy" />
      </label>

      <p v-if="!translation.available.value" class="mt-3 text-xs text-muted-foreground">
        {{ $t("translation.nothingUsable") }}
      </p>

      <h3 class="mt-5 text-xs font-semibold tracking-wide text-muted-foreground uppercase">
        {{ $t("translation.providers") }}
      </h3>
      <ul class="mt-2 divide-y rounded-lg border">
        <li
          v-for="state in overview.providers"
          :key="state.name"
          class="p-3"
          :class="{ 'opacity-70': !state.allowed }"
        >
          <div class="flex items-start justify-between gap-3">
            <div class="min-w-0">
              <p class="flex items-center gap-1.5 text-sm font-medium">
                {{ providerLabels[state.name] }}
                <Check
                  v-if="translation.provider.value === state.name"
                  class="size-3.5 text-primary"
                  :aria-label="$ts('translation.inUse')"
                />
              </p>
              <p class="mt-0.5 text-xs text-muted-foreground">{{ whoSees(state.name) }}</p>
              <p class="mt-1 text-xs">{{ status(state) }}</p>
            </div>

            <div
              v-if="state.allowed && state.name !== 'device' && editing !== state.name"
              class="flex shrink-0 items-center gap-1"
            >
              <Button
                variant="outline"
                size="small"
                :disabled="!overview.canStoreKeys && state.name !== 'libretranslate'"
                :label="`${own(state) ? $ts('translation.change') : $ts('translation.connect')}: ${providerLabels[state.name]}`"
                @click="editing = state.name"
              >
                {{ own(state) ? $t("translation.change") : $t("translation.connect") }}
              </Button>
              <Button
                v-if="own(state)"
                variant="danger"
                size="small"
                :label="`${$ts('translation.remove')}: ${providerLabels[state.name]}`"
                @click="removing = state.name"
              >
                {{ $t("translation.remove") }}
              </Button>
            </div>
          </div>

          <TranslationConnect
            v-if="editing === state.name"
            :provider="state"
            @done="editing = null"
            @cancel="editing = null"
          />
        </li>
      </ul>

      <p v-if="!overview.canStoreKeys" class="mt-2 text-xs text-muted-foreground">
        {{ $t("translation.cannotStoreKeys") }}
      </p>
    </template>

    <ConfirmDialog
      v-if="removing"
      :title="$ts('translation.removeTitle', { provider: providerLabels[removing] })"
      :detail="$ts('translation.removeDetail')"
      :confirm-label="$ts('translation.remove')"
      @cancel="removing = null"
      @confirm="confirmRemove"
    />
  </section>
</template>
