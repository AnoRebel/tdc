<script setup lang="ts">
import { useForm } from "@tanstack/vue-form";
import type { AnyFieldApi } from "@tanstack/vue-form";
import * as v from "valibot";

import { isApiError, type Project } from "@tdc/types";
import { Button } from "#layers/ui/app/components/ui/button";
import { Field } from "#layers/ui/app/components/ui/field";
import { Input } from "#layers/ui/app/components/ui/input";
import { Select } from "#layers/ui/app/components/ui/select";

const { $t, $ts, $localeRoute: localeRoute } = useI18n();

/**
 * Every project this person can see, and starting another.
 *
 * Private projects appear only to their members: to anybody else they are not
 * hidden but absent, which is the same rule channels follow.
 */

definePageMeta({ middleware: "auth", layout: "app" });
useHead({ title: $ts("projects.title") });

const projectsApi = useProjects();
const { can } = useAuth();
const router = useRouter();

const projects = ref<Project[]>([]);
const loading = ref(true);
const failed = ref<string | null>(null);
const starting = ref(false);

async function load() {
  loading.value = true;
  failed.value = null;
  try {
    projects.value = await projectsApi.list();
  } catch (cause) {
    failed.value = isApiError(cause) ? cause.message : $ts("projects.couldNotLoad");
  } finally {
    loading.value = false;
  }
}
onMounted(load);

/*
 * The rules a project must meet, stated once.
 *
 * The key is checked here for shape only; whether it is free is the server's
 * to say, since only the server knows every key — including the ones in
 * private projects nobody here can see.
 */
const schema = v.object({
  key: v.pipe(
    v.string(),
    v.trim(),
    v.toUpperCase(),
    v.regex(/^[A-Z][A-Z0-9]{1,9}$/, $ts("projects.keyRule")),
  ),
  name: v.pipe(v.string(), v.trim(), v.minLength(1, $ts("projects.nameRequired")), v.maxLength(80)),
  visibility: v.picklist(["public", "private"]),
});

const refused = ref<string | null>(null);

const form = useForm({
  defaultValues: { key: "", name: "", visibility: "public" as "public" | "private" },
  validators: { onSubmit: schema },
  async onSubmit({ value }) {
    refused.value = null;
    try {
      const project = await projectsApi.create({
        key: value.key.trim().toUpperCase(),
        name: value.name.trim(),
        visibility: value.visibility,
      });
      await router.push(localeRoute({ name: "projects-key", params: { key: project.key } }));
    } catch (cause) {
      refused.value = isApiError(cause) ? cause.message : $ts("projects.couldNotStart");
    }
  },
});

/*
 * A key suggested from the name, until somebody types one of their own.
 *
 * The initials of a name of several words, or the start of a single one —
 * "Community Website" becomes CW, "Mkutano" becomes MKU. Only a suggestion:
 * the moment the key field is edited, the name stops changing it.
 */
const keyEdited = ref(false);
function suggestKey(name: string): string {
  const words = name
    .toUpperCase()
    .replace(/[^A-Z0-9 ]/g, " ")
    .split(/\s+/)
    .filter(Boolean);
  const initials = words.length > 1 ? words.map((word) => word[0]).join("") : (words[0] ?? "");
  const key = (words.length > 1 ? initials : initials.slice(0, 3)).replace(/^[0-9]+/, "");
  return key.slice(0, 10);
}

function firstError(field: AnyFieldApi): string | undefined {
  if (!field.state.meta.isTouched) return undefined;
  const [issue] = field.state.meta.errors;
  if (!issue) return undefined;
  return typeof issue === "string" ? issue : (issue as { message?: string }).message;
}

const visibilities = computed(() => [
  { value: "public" as const, label: $ts("projects.public") },
  { value: "private" as const, label: $ts("projects.private") },
]);
</script>

<template>
  <div class="flex-1 overflow-y-auto">
    <header class="flex h-12 shrink-0 items-center gap-3 border-b px-4">
      <h1 class="text-sm font-semibold">{{ $t("projects.title") }}</h1>
      <Button
        v-if="can('project.create') && !starting"
        class="ml-auto"
        @click="((starting = true), form.reset())"
      >
        <Icon name="lucide:plus" class="size-3.5" />
        {{ $t("projects.start") }}
      </Button>
    </header>

    <div class="max-w-3xl space-y-6 px-4 py-6">
      <section
        v-if="starting"
        class="rounded-xl border bg-card p-4"
        :aria-label="$ts('projects.start')"
      >
        <h2 class="text-sm font-semibold">{{ $t("projects.start") }}</h2>
        <p class="mt-0.5 text-xs text-muted-foreground">{{ $t("projects.startHint") }}</p>

        <form class="mt-4 space-y-3" @submit.prevent="form.handleSubmit()">
          <form.Field name="name">
            <template #default="{ field }">
              <Field :label="$ts('projects.name')" :error="firstError(field)" required>
                <template #default="{ id, invalid, describedBy }">
                  <Input
                    :id="id"
                    :model-value="field.state.value"
                    :invalid="invalid"
                    :described-by="describedBy"
                    :placeholder="$ts('projects.namePlaceholder')"
                    @update:model-value="
                      (value: string) => {
                        field.handleChange(value);
                        if (!keyEdited) form.setFieldValue('key', suggestKey(value));
                      }
                    "
                    @blur="field.handleBlur"
                  />
                </template>
              </Field>
            </template>
          </form.Field>

          <form.Field name="key">
            <template #default="{ field }">
              <Field
                :label="$ts('projects.key')"
                :hint="$ts('projects.keyHint')"
                :error="firstError(field)"
                required
              >
                <template #default="{ id, invalid, describedBy }">
                  <Input
                    :id="id"
                    class="font-mono uppercase"
                    :model-value="field.state.value"
                    :invalid="invalid"
                    :described-by="describedBy"
                    placeholder="WEB"
                    @update:model-value="
                      (value: string) => {
                        keyEdited = true;
                        field.handleChange(value.toUpperCase());
                      }
                    "
                    @blur="field.handleBlur"
                  />
                </template>
              </Field>
            </template>
          </form.Field>

          <form.Field name="visibility">
            <template #default="{ field }">
              <Field :label="$ts('projects.visibility')" :hint="$ts('projects.visibilityHint')">
                <template #default="{ id }">
                  <Select
                    :id="id"
                    :model-value="field.state.value"
                    :label="$ts('projects.visibility')"
                    :options="visibilities"
                    @update:model-value="$event && field.handleChange($event)"
                  />
                </template>
              </Field>
            </template>
          </form.Field>

          <p v-if="refused" class="text-sm text-destructive" role="alert">{{ refused }}</p>

          <div class="flex justify-end gap-2">
            <Button variant="ghost" @click="starting = false">{{ $t("action.cancel") }}</Button>
            <Button type="submit">{{ $t("action.create") }}</Button>
          </div>
        </form>
      </section>

      <p v-if="failed" class="text-sm text-destructive" role="alert">{{ failed }}</p>

      <div v-else-if="loading" class="space-y-2" aria-busy="true">
        <UiSkeleton v-for="n in 3" :key="n" class="h-14 w-full rounded-lg" />
      </div>

      <div
        v-else-if="!projects.length"
        class="rounded-xl border border-dashed px-6 py-10 text-center"
      >
        <Icon name="lucide:kanban" class="mx-auto size-6 text-muted-foreground" />
        <p class="mt-2 text-sm font-medium">{{ $t("projects.noneYet") }}</p>
        <p class="mt-1 text-xs text-muted-foreground">{{ $t("projects.noneYetHint") }}</p>
      </div>

      <ul v-else class="space-y-1">
        <li v-for="project in projects" :key="project.id">
          <NuxtLink
            :to="localeRoute({ name: 'projects-key', params: { key: project.key } })"
            class="flex items-center gap-3 rounded-lg px-3 py-2.5 transition-colors hover:bg-accent"
          >
            <span
              class="grid h-7 min-w-10 place-items-center rounded-md bg-muted px-1.5 font-mono text-[0.6875rem] font-semibold"
            >
              {{ project.key }}
            </span>
            <span class="min-w-0 flex-1">
              <span class="block truncate text-sm font-medium">{{ project.name }}</span>
              <span v-if="project.description" class="block truncate text-xs text-muted-foreground">
                {{ project.description }}
              </span>
            </span>
            <Icon
              v-if="project.visibility === 'private'"
              name="lucide:lock"
              class="size-3.5 shrink-0 text-muted-foreground"
              :aria-label="$ts('projects.private')"
            />
            <span
              v-if="project.role"
              class="shrink-0 rounded bg-muted px-1.5 py-0.5 text-[0.6875rem] text-muted-foreground"
            >
              {{ project.role === "admin" ? $t("projects.roleAdmin") : $t("projects.roleMember") }}
            </span>
          </NuxtLink>
        </li>
      </ul>
    </div>
  </div>
</template>
