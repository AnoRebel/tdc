<script setup lang="ts">
import { isApiError, type Issue, type IssueHistoryEntry } from "@tdc/types";
import { Button } from "#layers/ui/app/components/ui/button";
import { Field } from "#layers/ui/app/components/ui/field";
import { Input } from "#layers/ui/app/components/ui/input";
import { Select } from "#layers/ui/app/components/ui/select";
import { Textarea } from "#layers/ui/app/components/ui/textarea";
import { fully } from "#layers/ui/app/lib/dates";

const { $t, $ts, $localeRoute: localeRoute } = useI18n();

/**
 * One issue: what it is, where it stands, and everything that happened to it.
 *
 * Addressed by its identifier — /issues/WEB-12 — which is the name people use
 * and the one thing about an issue that never changes.
 */

definePageMeta({ middleware: "auth", layout: "app" });

const route = useRoute();
const router = useRouter();
const ref_ = computed(() => String(route.params.ref).toUpperCase());
const projectsApi = useProjects();

const issue = ref<Issue | null>(null);
const history = ref<IssueHistoryEntry[]>([]);
const missing = ref(false);
const failed = ref<string | null>(null);
const saving = ref(false);

useHead({ title: computed(() => (issue.value ? `${issue.value.key} ${issue.value.title}` : "")) });

async function load() {
  failed.value = null;
  try {
    const [loaded, entries] = await Promise.all([
      projectsApi.issue(ref_.value),
      projectsApi.history(ref_.value),
    ]);
    issue.value = loaded;
    history.value = entries;
  } catch (cause) {
    if (isApiError(cause) && cause.status === 404) missing.value = true;
    else failed.value = isApiError(cause) ? cause.message : $ts("projects.couldNotLoad");
  }
}
onMounted(load);
watch(ref_, load);

const stateOptions = computed(() =>
  (issue.value?.project?.states ?? []).map((state) => ({ value: state.id, label: state.name })),
);

/*
 * Moving the issue sends the state it was seen in, so a move made against an
 * issue somebody else has just moved is refused rather than quietly
 * overwriting theirs — and the page then shows where it actually is.
 */
async function move(stateId: string | undefined) {
  const current = issue.value;
  if (!current || !stateId || stateId === current.state.id) return;
  saving.value = true;
  failed.value = null;
  try {
    await projectsApi.move(current.key, stateId, current.state.id);
  } catch (cause) {
    failed.value =
      isApiError(cause) && cause.status === 409
        ? $ts("projects.movedMeanwhile")
        : isApiError(cause)
          ? cause.message
          : $ts("projects.couldNotMove");
  } finally {
    saving.value = false;
    await load();
  }
}

/* Editing the title and description, in place. */
const editing = ref(false);
const draftTitle = ref("");
const draftDescription = ref("");

function startEditing() {
  if (!issue.value) return;
  draftTitle.value = issue.value.title;
  draftDescription.value = issue.value.description ?? "";
  editing.value = true;
}

async function saveEdits() {
  const current = issue.value;
  if (!current) return;
  if (!draftTitle.value.trim()) {
    failed.value = $ts("projects.titleRequired");
    return;
  }
  saving.value = true;
  failed.value = null;
  try {
    await projectsApi.edit(current.key, {
      title: draftTitle.value.trim(),
      description: draftDescription.value.trim(),
    });
    editing.value = false;
    await load();
  } catch (cause) {
    failed.value = isApiError(cause) ? cause.message : $ts("projects.couldNotSave");
  } finally {
    saving.value = false;
  }
}

const deleting = ref(false);
async function confirmDelete() {
  const current = issue.value;
  deleting.value = false;
  if (!current) return;
  try {
    await projectsApi.remove(current.key);
    await router.push(
      localeRoute({ name: "projects-key", params: { key: current.project?.key ?? "" } }),
    );
  } catch (cause) {
    failed.value = isApiError(cause) ? cause.message : $ts("projects.couldNotDelete");
  }
}
</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col">
    <div v-if="missing" class="grid flex-1 place-items-center p-6 text-center">
      <div>
        <p class="text-sm font-medium">{{ $t("projects.issueNotFound") }}</p>
        <NuxtLink
          :to="localeRoute({ name: 'projects' })"
          class="mt-2 inline-block text-xs text-primary hover:underline"
        >
          {{ $t("projects.all") }}
        </NuxtLink>
      </div>
    </div>

    <template v-else-if="issue">
      <header class="flex h-12 shrink-0 items-center gap-2 border-b px-4">
        <NuxtLink
          :to="localeRoute({ name: 'projects-key', params: { key: issue.project?.key ?? '' } })"
          class="btn btn-ghost size-7 px-0"
          :aria-label="$ts('projects.backToProject', { project: issue.project?.name ?? '' })"
        >
          <Icon name="lucide:arrow-left" class="size-4" />
        </NuxtLink>
        <NuxtLink
          :to="localeRoute({ name: 'projects-key', params: { key: issue.project?.key ?? '' } })"
          class="truncate text-xs text-muted-foreground hover:text-foreground"
        >
          {{ issue.project?.name }}
        </NuxtLink>
        <span class="text-xs text-muted-foreground" aria-hidden="true">/</span>
        <span class="font-mono text-xs font-semibold">{{ issue.key }}</span>
        <Button
          v-if="issue.can?.delete"
          variant="danger"
          size="small"
          class="ml-auto"
          @click="deleting = true"
        >
          {{ $t("action.delete") }}
        </Button>
      </header>

      <div class="flex-1 overflow-y-auto">
        <div class="mx-auto grid max-w-5xl gap-6 px-4 py-6 lg:grid-cols-[1fr_16rem]">
          <main class="min-w-0 space-y-6">
            <p v-if="failed" class="text-sm text-destructive" role="alert">{{ failed }}</p>

            <section v-if="editing" class="space-y-3" :aria-label="$ts('action.edit')">
              <Field :label="$ts('projects.issueTitle')">
                <template #default="{ id }">
                  <Input :id="id" v-model="draftTitle" class="text-base" />
                </template>
              </Field>
              <Field :label="$ts('projects.description')">
                <template #default="{ id }">
                  <Textarea :id="id" v-model="draftDescription" class="h-auto py-2" rows="8" />
                </template>
              </Field>
              <div class="flex justify-end gap-2">
                <Button variant="ghost" @click="editing = false">{{ $t("action.cancel") }}</Button>
                <Button :disabled="saving" @click="saveEdits">{{ $t("action.save") }}</Button>
              </div>
            </section>

            <section v-else>
              <div class="flex items-start gap-2">
                <h1 class="min-w-0 flex-1 text-lg font-semibold break-words">{{ issue.title }}</h1>
                <Button
                  v-if="issue.can?.edit"
                  variant="ghost"
                  size="icon"
                  :label="$ts('action.edit')"
                  @click="startEditing"
                >
                  <Icon name="lucide:pencil" class="size-3.5" />
                </Button>
              </div>
              <p
                v-if="issue.description"
                class="mt-3 text-sm leading-relaxed break-words whitespace-pre-wrap"
              >
                {{ issue.description }}
              </p>
              <p v-else class="mt-3 text-sm text-muted-foreground italic">
                {{ $t("projects.noDescription") }}
              </p>
            </section>

            <section :aria-label="$ts('projects.historyTitle')">
              <h2 class="mb-2 text-xs font-semibold text-muted-foreground uppercase">
                {{ $t("projects.historyTitle") }}
              </h2>
              <ProjectsIssueHistory :entries="history" />
            </section>
          </main>

          <aside class="space-y-4 text-sm" :aria-label="$ts('projects.details')">
            <Field v-if="issue.can?.transition" :label="$ts('projects.state')">
              <template #default="{ id }">
                <Select
                  :id="id"
                  :model-value="issue.state.id"
                  :label="$ts('projects.state')"
                  :options="stateOptions"
                  :disabled="saving"
                  @update:model-value="move"
                />
              </template>
            </Field>
            <div v-else>
              <h2 class="mb-1 text-xs font-semibold text-muted-foreground">
                {{ $t("projects.state") }}
              </h2>
              <ProjectsStateBadge :state="issue.state" />
            </div>
            <div>
              <h2 class="mb-1 text-xs font-semibold text-muted-foreground">
                {{ $t("projects.filedBy") }}
              </h2>
              <p class="text-xs">{{ issue.author }}</p>
              <p class="text-xs text-muted-foreground">{{ fully(issue.createdAt) }}</p>
            </div>
          </aside>
        </div>
      </div>
    </template>

    <div v-else class="space-y-2 p-4" aria-busy="true">
      <UiSkeleton class="h-6 w-2/3 rounded-md" />
      <UiSkeleton class="h-24 w-full rounded-md" />
    </div>

    <ConfirmDialog
      v-if="deleting && issue"
      :title="$ts('projects.deleteIssue', { issue: issue.key })"
      :detail="$ts('projects.deleteIssueHint')"
      :confirm-label="$ts('action.delete')"
      :cancel-label="$ts('action.cancel')"
      @cancel="deleting = false"
      @confirm="confirmDelete"
    />
  </div>
</template>
