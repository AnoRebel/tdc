<script setup lang="ts">
import type { Company, CompanyMember } from "@tdc/types";
import { isApiError } from "@tdc/types";
import { useForm } from "@tanstack/vue-form";
import type { AnyFieldApi } from "@tanstack/vue-form";
import * as v from "valibot";
import { ArrowLeft, Building2, MapPin, SearchX, UserPlus, X } from "lucide-vue-next";
import { Avatar } from "#layers/ui/app/components/ui/avatar";
import { Button } from "#layers/ui/app/components/ui/button";
import { Field } from "#layers/ui/app/components/ui/field";
import { Input } from "#layers/ui/app/components/ui/input";

const { $t, $ts, $localeRoute: localeRoute } = useI18n();

/**
 * A company's page.
 *
 * Everybody who may see it sees the people who agreed to be listed. Its
 * administrators also see who they have asked and not yet heard from — people
 * they invited, so nothing they did not already know — and may change the page.
 */

definePageMeta({ middleware: "auth", layout: "app" });

const route = useRoute();
const router = useRouter();
const directory = useDirectory();

const slug = computed(() => String(route.params.slug));
const company = ref<Company | null>(null);
const missing = ref(false);
const error = ref<string | null>(null);
const editing = ref(false);
const deleting = ref(false);
const removing = ref<CompanyMember | null>(null);

useHead({ title: () => company.value?.name ?? $ts("directory.company.companies") });

async function load() {
  missing.value = false;
  try {
    company.value = await directory.viewCompany(slug.value);
  } catch (cause) {
    company.value = null;
    if (isApiError(cause) && cause.code === "not-found") missing.value = true;
    else error.value = isApiError(cause) ? cause.message : $ts("directory.couldNotReach");
  }
}

function saved(updated: Company) {
  company.value = updated;
  editing.value = false;
}

async function confirmDelete() {
  deleting.value = false;
  try {
    await directory.deleteCompany(slug.value);
    await router.push(localeRoute({ name: "directory-profile" }));
  } catch (cause) {
    error.value = isApiError(cause) ? cause.message : $ts("directory.couldNotReach");
  }
}

async function confirmRemove() {
  const member = removing.value;
  removing.value = null;
  if (!member) return;
  try {
    await directory.removeFromCompany(slug.value, member.username);
    await load();
  } catch (cause) {
    error.value = isApiError(cause) ? cause.message : $ts("directory.couldNotReach");
  }
}

/*
 * Asking somebody to be listed. It lists nobody: they see the request on their
 * own profile page and decide.
 */
const invited = ref<string | null>(null);

function firstError(field: AnyFieldApi): string | undefined {
  if (!field.state.meta.isTouched) return undefined;
  const [issue] = field.state.meta.errors;
  if (!issue) return undefined;
  return typeof issue === "string" ? issue : (issue as { message?: string }).message;
}

const invite = useForm({
  defaultValues: { username: "", role: "" },
  validators: {
    onSubmit: v.object({
      username: v.pipe(v.string(), v.trim(), v.minLength(1, $ts("directory.company.whoToInvite"))),
      role: v.pipe(v.string(), v.maxLength(60, $ts("directory.form.tooLong", { count: 60 }))),
    }),
  },
  async onSubmit({ value, formApi }) {
    error.value = null;
    invited.value = null;
    try {
      await directory.inviteToCompany(slug.value, value.username.trim(), value.role.trim());
      invited.value = value.username.trim();
      formApi.reset();
      await load();
    } catch (cause) {
      error.value = isApiError(cause) ? cause.message : $ts("directory.couldNotReach");
    }
  },
});

// Last, so the form above is made while there is still a component instance
// for it to register its hooks with.
await load();
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
      <h1 class="truncate text-sm font-semibold">
        {{ company?.name ?? $t("directory.company.companies") }}
      </h1>
    </header>

    <div class="max-w-2xl space-y-6 px-4 py-6">
      <p
        v-if="error"
        class="rounded-md bg-destructive/10 px-3 py-2 text-sm text-destructive"
        role="alert"
      >
        {{ error }}
      </p>

      <div v-if="missing" class="rounded-lg border border-dashed py-12 text-center">
        <SearchX class="mx-auto size-6 text-muted-foreground/60" />
        <p class="mt-2 text-sm font-medium">{{ $t("directory.company.notFound") }}</p>
      </div>

      <template v-else-if="company">
        <DirectoryCompanyForm v-if="editing" :company="company" @saved="saved" />

        <section v-else class="flex items-start gap-4">
          <span class="grid size-12 shrink-0 place-items-center rounded-xl bg-muted">
            <Building2 class="size-5 text-muted-foreground" />
          </span>
          <div class="min-w-0 flex-1">
            <h2 class="text-lg font-semibold break-words">{{ company.name }}</h2>
            <p
              v-if="company.location"
              class="mt-0.5 flex items-center gap-1 text-xs text-muted-foreground"
            >
              <MapPin class="size-3.5" />
              {{ company.location }}
            </p>
            <p v-if="company.description" class="mt-2 text-sm whitespace-pre-wrap">
              {{ company.description }}
            </p>
          </div>
          <Button v-if="company.administered" variant="outline" @click="editing = true">
            {{ $t("directory.company.edit") }}
          </Button>
        </section>

        <section>
          <h2 class="text-xs font-semibold tracking-wide text-muted-foreground uppercase">
            {{ $t("directory.company.people") }}
          </h2>
          <ul class="mt-2 space-y-2">
            <li
              v-for="member in company.members"
              :key="member.username"
              class="flex items-center gap-3 rounded-lg border p-3"
            >
              <Avatar :name="member.displayName || member.username" size="sm" />
              <div class="min-w-0 flex-1">
                <NuxtLink
                  :to="
                    localeRoute({
                      name: 'directory-people-username',
                      params: { username: member.username },
                    })
                  "
                  class="text-sm font-medium hover:underline"
                >
                  {{ member.displayName || member.username }}
                </NuxtLink>
                <p class="text-xs text-muted-foreground">
                  {{ member.role || $t("directory.company.member") }}
                  <template v-if="member.administrator">
                    · {{ $t("directory.company.administrator") }}
                  </template>
                </p>
              </div>
              <Button
                v-if="company.administered"
                variant="ghost"
                size="icon"
                :label="$ts('directory.company.removeWho', { name: member.username })"
                @click="removing = member"
              >
                <X class="size-3.5" />
              </Button>
            </li>
          </ul>
        </section>

        <section v-if="company.administered" class="space-y-3 border-t pt-6">
          <h2 class="text-sm font-semibold">{{ $t("directory.company.invite") }}</h2>
          <p class="text-xs text-muted-foreground">{{ $t("directory.company.inviteHint") }}</p>

          <p v-if="invited" class="rounded-md bg-primary/10 px-3 py-2 text-sm" role="status">
            {{ $t("directory.company.invited", { name: invited }) }}
          </p>

          <form
            class="grid gap-3 sm:grid-cols-[1fr_1fr_auto] sm:items-end"
            novalidate
            @submit.prevent="invite.handleSubmit()"
          >
            <invite.Field name="username">
              <template #default="{ field }">
                <Field :label="$ts('auth.username')" :error="firstError(field)" required>
                  <template #default="{ id, invalid, describedBy }">
                    <Input
                      :id="id"
                      :model-value="field.state.value"
                      autocomplete="off"
                      :invalid="invalid"
                      :described-by="describedBy"
                      @update:model-value="field.handleChange($event)"
                      @blur="field.handleBlur"
                    />
                  </template>
                </Field>
              </template>
            </invite.Field>
            <invite.Field name="role">
              <template #default="{ field }">
                <Field :label="$ts('directory.company.role')" :error="firstError(field)">
                  <template #default="{ id, invalid, describedBy }">
                    <Input
                      :id="id"
                      :model-value="field.state.value"
                      :invalid="invalid"
                      :described-by="describedBy"
                      @update:model-value="field.handleChange($event)"
                      @blur="field.handleBlur"
                    />
                  </template>
                </Field>
              </template>
            </invite.Field>
            <Button type="submit" class="h-9">
              <UserPlus class="size-3.5" />
              {{ $t("directory.company.sendInvite") }}
            </Button>
          </form>

          <div v-if="company.pending?.length">
            <h3 class="text-xs font-semibold tracking-wide text-muted-foreground uppercase">
              {{ $t("directory.company.waitingFor") }}
            </h3>
            <ul class="mt-2 space-y-1 text-sm">
              <li
                v-for="member in company.pending"
                :key="member.username"
                class="flex items-center gap-2"
              >
                <span class="flex-1">{{ member.displayName || member.username }}</span>
                <Button variant="ghost" size="small" @click="removing = member">
                  {{ $t("directory.company.withdraw") }}
                </Button>
              </li>
            </ul>
          </div>

          <div class="flex justify-end border-t pt-4">
            <Button variant="danger" @click="deleting = true">
              {{ $t("directory.company.delete") }}
            </Button>
          </div>
        </section>
      </template>
    </div>

    <ConfirmDialog
      v-if="deleting"
      :title="$ts('directory.company.deleteTitle')"
      :detail="$ts('directory.company.deleteDetail')"
      :confirm-label="$ts('directory.company.delete')"
      @cancel="deleting = false"
      @confirm="confirmDelete"
    />
    <ConfirmDialog
      v-if="removing"
      :title="$ts('directory.company.removeTitle', { name: removing.username })"
      :detail="$ts('directory.company.removeDetail')"
      :confirm-label="$ts('directory.company.remove')"
      @cancel="removing = null"
      @confirm="confirmRemove"
    />
  </div>
</template>
