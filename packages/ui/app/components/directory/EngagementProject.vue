<script setup lang="ts">
import type { Engagement, Project } from "@tdc/types";
import { isApiError } from "@tdc/types";
import { Kanban, Link2Off } from "lucide-vue-next";
import { Button } from "#layers/ui/app/components/ui/button";
import { Select } from "#layers/ui/app/components/ui/select";

/**
 * Where an engagement's agreed work is tracked.
 *
 * A pointer to a project, offered to either party. Only projects the viewer is
 * in are offered, because linking says "this work is tracked there", which only
 * somebody in the project can say. The project's members see its issues as
 * they always do; nothing of the engagement — its proposals, prices or
 * conversation — goes with the link.
 */

const props = defineProps<{ engagement: Engagement }>();
const emit = defineEmits<{ changed: [engagement: Engagement] }>();

const { $t, $ts, $localeRoute: localeRoute } = useI18n();
const directory = useDirectory();
const projectsApi = useProjects();

const mine = ref<Project[]>([]);
const chosen = ref("");
const busy = ref(false);
const error = ref<string | null>(null);

const options = computed(() =>
  mine.value.map((project) => ({ value: project.key, label: `${project.key} · ${project.name}` })),
);

onMounted(async () => {
  if (props.engagement.project) return;
  try {
    mine.value = (await projectsApi.list()).filter((project) => Boolean(project.role));
    chosen.value = mine.value[0]?.key ?? "";
  } catch {
    // Nothing to offer is the same as no projects: the control says so.
  }
});

async function link() {
  if (!chosen.value) return;
  busy.value = true;
  error.value = null;
  try {
    emit("changed", await directory.linkProject(props.engagement.id, chosen.value));
  } catch (cause) {
    error.value = isApiError(cause) ? cause.message : $ts("directory.couldNotReach");
  } finally {
    busy.value = false;
  }
}

async function unlink() {
  busy.value = true;
  error.value = null;
  try {
    emit("changed", await directory.unlinkProject(props.engagement.id));
  } catch (cause) {
    error.value = isApiError(cause) ? cause.message : $ts("directory.couldNotReach");
  } finally {
    busy.value = false;
  }
}
</script>

<template>
  <div class="flex flex-wrap items-center gap-2 text-xs" data-engagement-project>
    <Kanban class="size-3.5 text-muted-foreground" aria-hidden="true" />
    <template v-if="engagement.project">
      <span class="text-muted-foreground">{{ $t("directory.engagement.trackedIn") }}</span>
      <NuxtLink
        :to="localeRoute({ name: 'projects-key', params: { key: engagement.project.key } })"
        class="font-medium hover:underline"
      >
        {{ engagement.project.key }} · {{ engagement.project.name }}
      </NuxtLink>
      <Button
        variant="ghost"
        size="small"
        class="h-6 px-1.5 text-xs"
        :disabled="busy"
        @click="unlink"
      >
        <Link2Off class="size-3.5" aria-hidden="true" />
        {{ $t("directory.engagement.unlinkProject") }}
      </Button>
    </template>
    <template v-else-if="options.length">
      <span class="text-muted-foreground">{{ $t("directory.engagement.trackIn") }}</span>
      <Select
        v-model="chosen"
        :label="$ts('directory.engagement.trackIn')"
        :options="options"
        class="h-7 w-48"
      />
      <Button size="small" class="h-7 text-xs" :disabled="busy || !chosen" @click="link">
        {{ $t("directory.engagement.linkProject") }}
      </Button>
    </template>
    <span v-else class="text-muted-foreground">{{
      $t("directory.engagement.noProjectToLink")
    }}</span>
    <p v-if="error" class="w-full text-destructive" role="alert">{{ error }}</p>
    <p class="w-full text-muted-foreground">{{ $t("directory.engagement.linkHint") }}</p>
  </div>
</template>
