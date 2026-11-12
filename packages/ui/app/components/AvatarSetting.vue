<script setup lang="ts">
import { ImageUp, Trash2 } from "lucide-vue-next";
import { isApiError } from "@tdc/types";

import { Avatar } from "#layers/ui/app/components/ui/avatar";
import { Button } from "#layers/ui/app/components/ui/button";
import { nameOf } from "#layers/ui/app/lib/names";

/**
 * Your picture: choose one, frame it, or go back to initials.
 *
 * The file chosen is only read in this browser, to be framed; what reaches
 * the server is the framed square. A file too large to frame comfortably on a
 * phone is refused before it is read at all.
 */

/** The largest file worth opening to frame: beyond this, a phone struggles. */
const LARGEST_FILE = 20 * 1024 * 1024;
const PICTURES = ["image/png", "image/jpeg", "image/webp", "image/gif"];

const { $t, $ts } = useI18n();
const api = useApi();
const { user } = useAuth();
const avatars = useAvatars();

const choosing = ref<File | null>(null);
const problem = ref("");
const removing = ref(false);

const has = computed(() => Boolean(avatars.pathOf(user.value?.address)));
const inputId = useId();

function chose(event: Event) {
  const input = event.target as HTMLInputElement;
  const file = input.files?.[0];
  // Cleared, so choosing the same file again still counts as a choice.
  input.value = "";
  problem.value = "";
  if (!file) return;
  if (!PICTURES.includes(file.type)) {
    problem.value = $ts("avatar.notAPicture");
    return;
  }
  if (file.size > LARGEST_FILE) {
    problem.value = $ts("avatar.tooLarge", {
      size: (file.size / 1024 / 1024).toFixed(0),
      limit: String(LARGEST_FILE / 1024 / 1024),
    });
    return;
  }
  choosing.value = file;
}

async function remove() {
  removing.value = true;
  problem.value = "";
  try {
    await api.delete("/api/me/avatar");
    if (user.value) avatars.setOwn(user.value.address, "");
  } catch (cause) {
    problem.value = isApiError(cause) ? cause.message : $ts("avatar.couldNotSave");
  } finally {
    removing.value = false;
  }
}
</script>

<template>
  <section>
    <h2 class="text-sm font-semibold">{{ $t("avatar.title") }}</h2>
    <p class="mt-0.5 text-xs text-muted-foreground">{{ $t("avatar.hint") }}</p>

    <div class="mt-3 flex flex-wrap items-center gap-3">
      <Avatar
        :address="user?.address"
        :name="user ? nameOf(user) : ''"
        size="lg"
        class="size-16 text-lg"
      />
      <label
        :for="inputId"
        class="btn btn-outline cursor-pointer has-focus-visible:ring-2 has-focus-visible:ring-ring"
      >
        <ImageUp class="size-3.5" aria-hidden="true" />
        {{ has ? $t("avatar.change") : $t("avatar.choose") }}
        <input
          :id="inputId"
          type="file"
          class="sr-only"
          :accept="PICTURES.join(',')"
          @change="chose"
        />
      </label>
      <Button v-if="has" variant="ghost" :disabled="removing" @click="remove">
        <Trash2 class="size-3.5" aria-hidden="true" />
        {{ $t("avatar.remove") }}
      </Button>
    </div>

    <p v-if="problem" role="alert" class="mt-2 text-xs text-destructive">{{ problem }}</p>

    <LazyAvatarEditor
      v-if="choosing"
      :file="choosing"
      @done="choosing = null"
      @cancel="choosing = null"
    />
  </section>
</template>
