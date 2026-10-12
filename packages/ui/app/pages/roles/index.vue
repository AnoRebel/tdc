<script setup lang="ts">
import { useForm } from "@tanstack/vue-form";
import type { AnyFieldApi } from "@tanstack/vue-form";
import * as v from "valibot";
import { ArrowLeft, ChevronRight, Plus, ShieldCheck, Users } from "lucide-vue-next";

import { isApiError } from "@tdc/types";
import { Button } from "#layers/ui/app/components/ui/button";
import { Field } from "#layers/ui/app/components/ui/field";
import { Input } from "#layers/ui/app/components/ui/input";
import { Skeleton } from "#layers/ui/app/components/ui/skeleton";
import { Spinner } from "#layers/ui/app/components/ui/spinner";
import { Textarea } from "#layers/ui/app/components/ui/textarea";
import type { Role } from "#layers/ui/app/lib/roles";

const { $t, $ts, $localeRoute: localeRoute } = useI18n();

/**
 * Every role on the server: who holds it, what it says, and a way into each.
 *
 * Open to whoever makes roles and whoever hands them out. The server checks
 * every change regardless; what is decided here is only what to offer, so
 * somebody who may assign but not edit sees the list and no way to make one.
 */

definePageMeta({ middleware: "auth", layout: "app" });

useHead({ title: $ts("roles.title") });

const roles = useRoles();
const { can } = useAuth();
const router = useRouter();

onMounted(() => void roles.refresh());

const refused = computed(
  () => isApiError(roles.failure.value) && roles.failure.value.status === 403,
);

/** A role's entries in three numbers, which is what the list has room for. */
function summary(role: Role) {
  const server = role.permissions.filter((entry) => entry.scope === "server");
  return {
    allowed: server.filter((entry) => entry.granted).length,
    denied: server.filter((entry) => !entry.granted).length,
    scoped: role.permissions.length - server.length,
  };
}

const creating = ref(false);
const error = ref<string | null>(null);

const details = v.object({
  name: v.pipe(
    v.string(),
    v.trim(),
    v.minLength(1, $ts("roles.nameRequired")),
    v.maxLength(64, $ts("roles.nameTooLong")),
  ),
  description: v.pipe(v.string(), v.trim(), v.maxLength(500, $ts("roles.descriptionTooLong"))),
});

function firstError(field: AnyFieldApi): string | undefined {
  if (!field.state.meta.isTouched) return undefined;
  const [issue] = field.state.meta.errors;
  if (!issue) return undefined;
  return typeof issue === "string" ? issue : (issue as { message?: string }).message;
}

const form = useForm({
  defaultValues: { name: "", description: "" },
  validators: { onSubmit: details },
  async onSubmit({ value }) {
    error.value = null;
    try {
      const role = await roles.create(value.name.trim(), value.description.trim());
      creating.value = false;
      form.reset();
      await router.push(localeRoute({ name: "roles-id", params: { id: role.id } }));
    } catch (cause) {
      error.value = isApiError(cause) ? cause.message : $ts("roles.notCreated");
    }
  },
});

function lookUp(choice: { id: string }) {
  void router.push(localeRoute({ name: "roles-people-id", params: { id: choice.id } }));
}
</script>

<template>
  <div class="flex-1 overflow-y-auto">
    <header class="flex h-12 shrink-0 items-center gap-3 border-b px-4">
      <NuxtLink
        :to="localeRoute({ name: 'settings' })"
        class="btn btn-ghost size-7 px-0"
        :aria-label="$ts('admin.backToSettings')"
      >
        <ArrowLeft class="size-4" />
      </NuxtLink>
      <h1 class="text-sm font-semibold">{{ $t("roles.title") }}</h1>
    </header>

    <div class="max-w-3xl space-y-8 px-4 py-6">
      <RolesRefusal v-if="refused" />

      <div
        v-else-if="roles.failure.value"
        class="flex items-center justify-between gap-3 rounded-lg border border-destructive/30 bg-destructive/8 p-3 text-sm"
        role="alert"
      >
        <span class="text-destructive">{{ $t("roles.loadFailed") }}</span>
        <Button variant="outline" @click="roles.refresh()">{{ $t("action.retry") }}</Button>
      </div>

      <template v-else>
        <section>
          <div class="flex items-start justify-between gap-4">
            <p class="text-xs text-muted-foreground">{{ $t("roles.intro") }}</p>
            <Button
              v-if="can('role.manage') && !creating"
              class="shrink-0"
              @click="creating = true"
            >
              <Plus class="size-3.5" aria-hidden="true" />
              {{ $t("roles.newRole") }}
            </Button>
          </div>

          <Transition
            enter-active-class="transition-[opacity,transform] duration-(--motion-duration) ease-(--motion-ease-out)"
            enter-from-class="opacity-0 -translate-y-1"
          >
            <form
              v-if="creating"
              class="mt-3 space-y-3 rounded-lg border bg-surface p-3"
              :aria-label="$ts('roles.newRole')"
              @submit.prevent="form.handleSubmit()"
            >
              <form.Field name="name">
                <template #default="{ field }">
                  <Field :label="$ts('roles.name')" :error="firstError(field)" required>
                    <template #default="{ id, invalid, describedBy }">
                      <Input
                        :id="id"
                        :model-value="field.state.value"
                        :placeholder="$ts('roles.namePlaceholder')"
                        :invalid="invalid"
                        :described-by="describedBy"
                        autofocus
                        @update:model-value="field.handleChange($event)"
                        @blur="field.handleBlur"
                      />
                    </template>
                  </Field>
                </template>
              </form.Field>
              <form.Field name="description">
                <template #default="{ field }">
                  <Field
                    :label="$ts('roles.description')"
                    :hint="$ts('roles.descriptionHint')"
                    :error="firstError(field)"
                  >
                    <template #default="{ id, describedBy }">
                      <Textarea
                        :id="id"
                        class="h-auto py-2"
                        rows="2"
                        :model-value="field.state.value"
                        :aria-describedby="describedBy"
                        @update:model-value="field.handleChange(String($event))"
                        @blur="field.handleBlur"
                      />
                    </template>
                  </Field>
                </template>
              </form.Field>
              <p v-if="error" class="text-xs text-destructive" role="alert">{{ error }}</p>
              <div class="flex justify-end gap-2">
                <Button variant="ghost" @click="creating = false">{{ $t("action.cancel") }}</Button>
                <Button type="submit" :disabled="form.state.isSubmitting">
                  <Spinner v-if="form.state.isSubmitting" size="sm" />
                  {{ $t("roles.create") }}
                </Button>
              </div>
            </form>
          </Transition>

          <ul v-if="roles.loading.value && !roles.roles.value.length" class="mt-3 space-y-1.5">
            <li v-for="row in 3" :key="row"><Skeleton class="h-14 w-full" /></li>
          </ul>

          <p v-else-if="!roles.roles.value.length" class="mt-3 text-xs text-muted-foreground">
            {{ $t("roles.noRoles") }}
          </p>

          <ul v-else class="mt-3 divide-y rounded-lg border bg-surface">
            <li v-for="role in roles.roles.value" :key="role.id">
              <NuxtLink
                :to="localeRoute({ name: 'roles-id', params: { id: role.id } })"
                class="group flex items-center gap-3 px-3 py-2.5 transition-colors duration-(--motion-duration-fast) hover:bg-accent focus-visible:bg-accent focus-visible:outline-none"
              >
                <ShieldCheck class="size-4 shrink-0 text-muted-foreground" aria-hidden="true" />
                <span class="min-w-0 flex-1">
                  <span class="flex items-center gap-1.5 text-sm font-medium">
                    {{ role.name }}
                    <span
                      v-if="role.builtIn"
                      class="rounded bg-muted px-1.5 py-0.5 text-[0.6875rem] font-normal text-muted-foreground"
                    >
                      {{ $t("roles.builtIn") }}
                    </span>
                  </span>
                  <span class="block truncate text-xs text-muted-foreground">
                    {{ role.description || $t("roles.noDescription") }}
                  </span>
                </span>
                <span class="hidden shrink-0 gap-2 text-[0.6875rem] text-muted-foreground sm:flex">
                  <span class="tabular">{{
                    $t("roles.summaryAllowed", { count: summary(role).allowed })
                  }}</span>
                  <span v-if="summary(role).denied" class="tabular text-destructive">{{
                    $t("roles.summaryDenied", { count: summary(role).denied })
                  }}</span>
                  <span v-if="summary(role).scoped" class="tabular">{{
                    $t("roles.summaryScoped", { count: summary(role).scoped })
                  }}</span>
                </span>
                <span
                  class="tabular flex w-12 shrink-0 items-center justify-end gap-1 text-xs text-muted-foreground"
                  :aria-label="$ts('roles.holders', { count: role.memberCount })"
                >
                  <Users class="size-3.5" aria-hidden="true" />
                  {{ role.memberCount }}
                </span>
                <ChevronRight
                  class="size-4 shrink-0 text-muted-foreground transition-transform duration-(--motion-duration-fast) group-hover:translate-x-0.5"
                  aria-hidden="true"
                />
              </NuxtLink>
            </li>
          </ul>
        </section>

        <section>
          <h2 class="text-sm font-semibold">{{ $t("roles.lookUp") }}</h2>
          <p class="mt-0.5 text-xs text-muted-foreground">{{ $t("roles.lookUpHint") }}</p>
          <RolesSearchPicker
            class="mt-3"
            kind="people"
            :label="$ts('roles.findPerson')"
            :placeholder="$ts('roles.findPerson')"
            @pick="lookUp"
          />
        </section>
      </template>
    </div>
  </div>
</template>
