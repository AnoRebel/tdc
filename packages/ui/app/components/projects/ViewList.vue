<script setup lang="ts">
import { isApiError, type SavedView } from "@tdc/types";
import { Button } from "#layers/ui/app/components/ui/button";

const { $t, $ts, $localeRoute: localeRoute } = useI18n();

/**
 * Saved views somebody may open: their own, and those shared with them.
 *
 * Each says whose it is and whether it is shared, because a shared view is
 * somebody else's question — opening it is fine, changing it is theirs.
 */
const props = defineProps<{ views: SavedView[] }>();
const emit = defineEmits<{ removed: [id: string] }>();

const projectsApi = useProjects();
const failed = ref<string | null>(null);

async function remove(view: SavedView) {
  failed.value = null;
  try {
    await projectsApi.deleteView(view.id);
    emit("removed", view.id);
  } catch (cause) {
    failed.value = isApiError(cause) ? cause.message : $ts("projects.saved.couldNotDelete");
  }
}
</script>

<template>
  <div>
    <p v-if="!props.views.length" class="text-sm text-muted-foreground">
      {{ $t("projects.saved.none") }}
    </p>
    <ul v-else class="divide-y rounded-lg border bg-surface">
      <li v-for="view in props.views" :key="view.id" class="flex items-center gap-2 px-3 py-2">
        <Icon
          :name="view.layout === 'board' ? 'lucide:columns-3' : 'lucide:list'"
          class="size-3.5 shrink-0 text-muted-foreground"
          aria-hidden="true"
        />
        <NuxtLink
          :to="localeRoute({ name: 'views-id', params: { id: view.id } })"
          class="min-w-0 flex-1 truncate text-sm font-medium hover:underline"
        >
          {{ view.name }}
        </NuxtLink>
        <span v-if="view.project" class="font-mono text-xs text-muted-foreground">
          {{ view.project }}
        </span>
        <span
          v-if="view.shared"
          class="rounded border px-1.5 py-0.5 text-[0.65rem] font-medium text-muted-foreground"
        >
          {{ $t("projects.saved.shared") }}
        </span>
        <span v-if="!view.mine" class="text-xs text-muted-foreground">
          {{ $t("projects.saved.by", { owner: view.owner }) }}
        </span>
        <Button
          v-if="view.mine"
          variant="danger"
          size="icon"
          :label="$ts('projects.saved.delete', { name: view.name })"
          @click="remove(view)"
        >
          <Icon name="lucide:trash-2" class="size-3.5" />
        </Button>
      </li>
    </ul>
    <p v-if="failed" class="mt-2 text-sm text-destructive" role="alert">{{ failed }}</p>
  </div>
</template>
