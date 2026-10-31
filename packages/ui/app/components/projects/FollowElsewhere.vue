<script setup lang="ts">
import { isApiError, type PublishedProject } from "@tdc/types";
import { Button } from "#layers/ui/app/components/ui/button";
import { Field } from "#layers/ui/app/components/ui/field";
import { Input } from "#layers/ui/app/components/ui/input";

const { $ts } = useI18n();

/**
 * Following a public project on another server.
 *
 * The other server decides what it lists to this one, so an empty answer
 * means nothing to follow — whether because there is nothing, it lists
 * nothing to us, or it cannot be reached. A project followed is held here for
 * everybody to read, and arrives a moment after its home agrees.
 */

const emit = defineEmits<{ close: []; followed: [] }>();
const projectsApi = useProjects();

const server = ref("");
const asked = ref("");
const found = ref<PublishedProject[] | null>(null);
const looking = ref(false);
const failed = ref<string | null>(null);
const following = ref(new Set<string>());

async function look() {
  const named = server.value.trim();
  if (!named) return;
  looking.value = true;
  failed.value = null;
  try {
    found.value = await projectsApi.elsewhere(named);
    asked.value = named;
  } catch (cause) {
    failed.value = isApiError(cause) ? cause.message : $ts("projects.elsewhere.couldNotLook");
  } finally {
    looking.value = false;
  }
}

async function follow(project: PublishedProject) {
  failed.value = null;
  try {
    await projectsApi.follow(asked.value, project);
    following.value = new Set([...following.value, project.id]);
    emit("followed");
  } catch (cause) {
    failed.value = isApiError(cause) ? cause.message : $ts("projects.elsewhere.couldNotFollow");
  }
}
</script>

<template>
  <section class="rounded-xl border bg-card p-4" :aria-label="$ts('projects.elsewhere.title')">
    <div class="flex items-start gap-2">
      <div class="min-w-0 flex-1">
        <h2 class="text-sm font-semibold">{{ $t("projects.elsewhere.title") }}</h2>
        <p class="mt-0.5 text-xs text-muted-foreground">{{ $t("projects.elsewhere.hint") }}</p>
      </div>
      <Button variant="ghost" size="small" @click="emit('close')">
        {{ $t("action.close") }}
      </Button>
    </div>

    <form class="mt-3 flex items-end gap-2" @submit.prevent="look">
      <Field :label="$ts('projects.elsewhere.server')" class="flex-1">
        <template #default="{ id, describedBy }">
          <Input
            :id="id"
            v-model="server"
            :described-by="describedBy"
            placeholder="example.org"
            autocomplete="off"
          />
        </template>
      </Field>
      <Button type="submit" variant="outline" :disabled="looking || !server.trim()">
        <Icon name="lucide:search" class="size-3.5" aria-hidden="true" />
        {{ $t("projects.elsewhere.look") }}
      </Button>
    </form>

    <p
      v-if="failed"
      role="alert"
      class="mt-3 rounded-md bg-destructive/10 px-2 py-1 text-xs text-destructive"
    >
      {{ failed }}
    </p>

    <template v-if="found">
      <p v-if="!found.length" class="mt-3 text-xs text-muted-foreground">
        {{ $t("projects.elsewhere.none", { server: asked }) }}
      </p>
      <ul v-else class="mt-3 divide-y rounded-lg border">
        <li v-for="project in found" :key="project.id" class="flex items-center gap-3 px-3 py-2">
          <span
            class="rounded bg-muted px-1.5 py-0.5 font-mono text-[0.6875rem] font-semibold text-muted-foreground"
          >
            {{ project.key }}
          </span>
          <span class="min-w-0 flex-1">
            <span class="block truncate text-sm font-medium">{{ project.name }}</span>
            <span v-if="project.description" class="block truncate text-xs text-muted-foreground">{{
              project.description
            }}</span>
          </span>
          <span
            v-if="following.has(project.id)"
            role="status"
            class="flex items-center gap-1 text-xs text-muted-foreground"
          >
            <Icon name="lucide:check" class="size-3.5" aria-hidden="true" />
            {{ $t("projects.elsewhere.following") }}
          </span>
          <Button
            v-else
            size="small"
            variant="outline"
            :label="$ts('projects.elsewhere.followProject', { project: project.name })"
            @click="follow(project)"
          >
            {{ $t("projects.elsewhere.follow") }}
          </Button>
        </li>
      </ul>
    </template>
  </section>
</template>
