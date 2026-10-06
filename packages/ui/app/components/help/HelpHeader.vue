<script setup lang="ts">
import { LifeBuoy, Menu } from "lucide-vue-next";
import type { Ref } from "vue";

import { Button } from "#layers/ui/app/components/ui/button";

/**
 * The bar across the top of every help page: where you are, and the way back.
 *
 * The trail is short on purpose — Help, then the page — because the help
 * centre is two levels deep and a longer one would be decoration.
 */
const props = defineProps<{
  /** The page's own name, after "Help". Absent on the index itself. */
  current?: string;
}>();

const { $t, $ts, $localeRoute: localeRoute } = useI18n();

// The layout's drawer, on a screen narrow enough for the sidebar to be one.
const navigationOpen = inject<Ref<boolean>>("navigationOpen", ref(false));
</script>

<template>
  <header
    class="sticky top-0 z-10 flex h-12 shrink-0 items-center gap-2 border-b bg-background/85 px-3 backdrop-blur md:px-4"
  >
    <Button
      variant="ghost"
      size="icon"
      class="shrink-0 md:hidden"
      :label="$ts('nav.channels')"
      @click="navigationOpen = true"
    >
      <Menu class="size-4" />
    </Button>

    <LifeBuoy class="size-4 shrink-0 text-muted-foreground" aria-hidden="true" />
    <nav :aria-label="$ts('help.title')" class="min-w-0">
      <ol class="flex min-w-0 items-center gap-1.5 text-sm">
        <li class="shrink-0">
          <NuxtLink
            v-if="props.current"
            :to="localeRoute({ name: 'help' })"
            class="text-muted-foreground hover:text-foreground"
          >
            {{ $t("help.title") }}
          </NuxtLink>
          <h1 v-else class="font-semibold">{{ $t("help.title") }}</h1>
        </li>
        <template v-if="props.current">
          <li aria-hidden="true" class="text-muted-foreground/60">/</li>
          <li class="min-w-0 truncate font-semibold" aria-current="page">{{ props.current }}</li>
        </template>
      </ol>
    </nav>

    <div class="ml-auto flex items-center gap-1">
      <slot />
    </div>
  </header>
</template>
