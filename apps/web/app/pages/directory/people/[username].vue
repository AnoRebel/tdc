<script setup lang="ts">
import type { ProfileView } from "@tdc/types";
import { isApiError } from "@tdc/types";
import { ArrowLeft, SearchX } from "lucide-vue-next";

const { $t, $ts, $localeRoute: localeRoute } = useI18n();

/**
 * One person's directory profile, as the signed-in person may see it.
 *
 * "Not found" covers everything the viewer may not learn — no such person, no
 * profile, a hidden one — and is shown the same way for all of them, because
 * the server answers the same way for all of them.
 */

definePageMeta({ middleware: "auth", layout: "app" });

const route = useRoute();
const directory = useDirectory();
const { user } = useAuth();

const username = computed(() => String(route.params.username));
const profile = ref<ProfileView | null>(null);
const missing = ref(false);
const error = ref<string | null>(null);

useHead({ title: () => profile.value?.displayName || $ts("directory.title") });

async function load() {
  missing.value = false;
  error.value = null;
  try {
    profile.value = await directory.viewProfile(username.value);
  } catch (cause) {
    profile.value = null;
    if (isApiError(cause) && cause.code === "not-found") missing.value = true;
    else error.value = isApiError(cause) ? cause.message : $ts("directory.couldNotReach");
  }
}

await load();
watch(username, load);

const isMe = computed(() => user.value?.username === profile.value?.username);
</script>

<template>
  <div class="flex-1 overflow-y-auto">
    <header class="flex h-12 items-center gap-3 border-b px-4">
      <NuxtLink
        :to="localeRoute({ name: 'directory' })"
        class="btn btn-ghost size-7 px-0"
        :aria-label="$ts('directory.backToDirectory')"
      >
        <ArrowLeft class="size-4" />
      </NuxtLink>
      <h2 class="text-sm font-semibold">{{ $t("directory.title") }}</h2>
    </header>

    <div class="max-w-2xl px-4 py-6">
      <p
        v-if="error"
        class="rounded-md bg-destructive/10 px-3 py-2 text-sm text-destructive"
        role="alert"
      >
        {{ error }}
      </p>

      <div v-else-if="missing" class="rounded-lg border border-dashed py-12 text-center">
        <SearchX class="mx-auto size-6 text-muted-foreground/60" />
        <p class="mt-2 text-sm font-medium">{{ $t("directory.profile.notFound") }}</p>
        <p class="mt-0.5 text-xs text-muted-foreground">
          {{ $t("directory.profile.notFoundHint") }}
        </p>
      </div>

      <DirectoryProfileCard v-else-if="profile" :profile="profile">
        <template #actions>
          <p v-if="isMe" class="rounded-md bg-muted/60 px-3 py-2 text-xs text-muted-foreground">
            {{ $t("directory.profile.thisIsYou") }}
            <NuxtLink
              :to="localeRoute({ name: 'directory-profile' })"
              class="font-medium text-primary hover:underline"
            >
              {{ $t("directory.profile.edit") }}
            </NuxtLink>
          </p>
        </template>
      </DirectoryProfileCard>
    </div>
  </div>
</template>
