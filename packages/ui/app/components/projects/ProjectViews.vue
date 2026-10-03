<script setup lang="ts">
import { isApiError, type SavedView, type ViewFilter } from "@tdc/types";
import { Button } from "#layers/ui/app/components/ui/button";
import SaveView from "#layers/ui/app/components/projects/SaveView.vue";
import ViewList from "#layers/ui/app/components/projects/ViewList.vue";

const { $t, $ts } = useI18n();

/**
 * A project's saved views, and saving what is on the screen as another.
 *
 * Closed until asked for: most visits to a list or a board are to read it,
 * and the views are a way back to it rather than part of it.
 */
const props = defineProps<{
  projectKey: string;
  layout: "list" | "board";
  filter: ViewFilter;
  mayShare: boolean;
}>();

const projectsApi = useProjects();
const open = ref(false);
const saving = ref(false);
const views = ref<SavedView[] | null>(null);
const failed = ref<string | null>(null);
const uid = useId();

async function load() {
  failed.value = null;
  try {
    views.value = await projectsApi.views(props.projectKey);
  } catch (cause) {
    failed.value = isApiError(cause) ? cause.message : $ts("projects.couldNotLoad");
  }
}

watch(open, (now) => {
  if (now && !views.value) void load();
});

function saved(view: SavedView) {
  saving.value = false;
  open.value = true;
  views.value = [...(views.value ?? []), view].sort((a, b) => a.name.localeCompare(b.name));
}
</script>

<template>
  <div class="space-y-3">
    <div class="flex flex-wrap items-center gap-2">
      <Button
        variant="outline"
        :aria-expanded="open"
        :aria-controls="`${uid}-views`"
        @click="open = !open"
      >
        <Icon name="lucide:bookmark" class="size-3.5" />
        {{ $t("projects.saved.title") }}
      </Button>
      <Button v-if="!saving" variant="ghost" @click="saving = true">
        <Icon name="lucide:bookmark-plus" class="size-3.5" />
        {{ $t("projects.saved.save") }}
      </Button>
    </div>
    <SaveView
      v-if="saving"
      :project-key="props.projectKey"
      :layout="props.layout"
      :filter="props.filter"
      :may-share="props.mayShare"
      @saved="saved"
      @cancel="saving = false"
    />
    <div v-if="open" :id="`${uid}-views`" class="max-w-2xl">
      <p v-if="failed" class="text-sm text-destructive" role="alert">{{ failed }}</p>
      <ViewList
        v-else-if="views"
        :views="views"
        @removed="(id) => (views = (views ?? []).filter((view) => view.id !== id))"
      />
    </div>
  </div>
</template>
