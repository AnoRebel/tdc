<script setup lang="ts">
import type { DirectoryFilters, DirectoryHit } from "@tdc/types";
import { isApiError } from "@tdc/types";
import { useForm } from "@tanstack/vue-form";
import { BriefcaseBusiness, Contact, MapPin, Search, SearchX, UserRound } from "lucide-vue-next";
import { Avatar } from "#layers/ui/app/components/ui/avatar";
import { Button } from "#layers/ui/app/components/ui/button";
import { Checkbox } from "#layers/ui/app/components/ui/checkbox";
import { Field } from "#layers/ui/app/components/ui/field";
import { Input } from "#layers/ui/app/components/ui/input";
import { Spinner } from "#layers/ui/app/components/ui/spinner";

const { $t, $ts, $localeRoute: localeRoute } = useI18n();

/**
 * The directory: people who chose to be found.
 *
 * Browsing is a search with nothing filled in. Results come a page at a time,
 * at a size the server decides, and filters apply only to what each person
 * chose to show — somebody whose location is hidden from you is not found by
 * searching for their town, because finding them that way would tell you
 * where they are.
 *
 * The filters live in the address, so a search can be shared and survives a
 * reload.
 */

definePageMeta({ middleware: "auth", layout: "app" });

useHead({ title: $ts("directory.title") });

const route = useRoute();
const router = useRouter();
const directory = useDirectory();
const { can } = useAuth();

function fromQuery(): Required<Omit<DirectoryFilters, "page">> {
  const text = (key: string) => (typeof route.query[key] === "string" ? route.query[key] : "");
  return {
    q: text("q"),
    skill: text("skill"),
    location: text("location"),
    company: text("company"),
    available: route.query.available === "true",
  };
}

const availableId = useId();
const hits = ref<DirectoryHit[]>([]);
const page = ref(0);
const more = ref(false);
const loading = ref(false);
const error = ref<string | null>(null);

async function run(filters: DirectoryFilters, append = false) {
  loading.value = true;
  error.value = null;
  try {
    const results = await directory.search(filters);
    hits.value = append ? [...hits.value, ...results.profiles] : results.profiles;
    page.value = results.page;
    more.value = results.more;
  } catch (cause) {
    error.value =
      isApiError(cause) && cause.code === "rate-limited"
        ? $ts("directory.search.slowDown")
        : isApiError(cause)
          ? cause.message
          : $ts("directory.couldNotReach");
  } finally {
    loading.value = false;
  }
}

const form = useForm({
  defaultValues: fromQuery(),
  async onSubmit({ value }) {
    const query: Record<string, string> = {};
    for (const [key, entry] of Object.entries(value)) {
      if (entry === true) query[key] = "true";
      else if (typeof entry === "string" && entry.trim()) query[key] = entry.trim();
    }
    await router.replace({ query });
    await run(value);
  },
});

await run(fromQuery());

async function loadMore() {
  await run({ ...fromQuery(), page: page.value + 1 }, true);
}

const filtered = computed(() => Object.values(fromQuery()).some(Boolean));
</script>

<template>
  <div class="flex-1 overflow-y-auto">
    <header class="flex h-12 items-center gap-3 border-b px-4">
      <Contact class="size-4 text-muted-foreground" />
      <h1 class="text-sm font-semibold">{{ $t("directory.title") }}</h1>
      <div class="ml-auto flex items-center gap-1">
        <NuxtLink
          v-if="can('directory.skills.manage') || can('directory.verify')"
          :to="localeRoute({ name: 'directory-review' })"
          class="btn btn-ghost"
        >
          {{ $t("directory.review.title") }}
        </NuxtLink>
        <NuxtLink :to="localeRoute({ name: 'directory-enquiries' })" class="btn btn-ghost">
          {{ $t("directory.enquiry.title") }}
        </NuxtLink>
        <NuxtLink :to="localeRoute({ name: 'directory-profile' })" class="btn btn-ghost">
          <UserRound class="size-3.5" />
          {{ $t("directory.profile.yours") }}
        </NuxtLink>
      </div>
    </header>

    <div class="max-w-3xl space-y-6 px-4 py-6">
      <p class="text-sm text-muted-foreground">{{ $t("directory.intro") }}</p>

      <form
        class="space-y-3 rounded-lg border p-3"
        role="search"
        :aria-label="$ts('directory.search.label')"
        @submit.prevent="form.handleSubmit()"
      >
        <form.Field name="q">
          <template #default="{ field }">
            <Field :label="$ts('directory.search.query')">
              <template #default="{ id }">
                <Input
                  :id="id"
                  type="search"
                  :model-value="field.state.value"
                  :placeholder="$ts('directory.search.queryHint')"
                  @update:model-value="field.handleChange($event)"
                />
              </template>
            </Field>
          </template>
        </form.Field>

        <div class="grid gap-3 sm:grid-cols-3">
          <form.Field name="skill">
            <template #default="{ field }">
              <Field :label="$ts('directory.search.skill')">
                <template #default="{ id }">
                  <Input
                    :id="id"
                    :model-value="field.state.value"
                    @update:model-value="field.handleChange($event)"
                  />
                </template>
              </Field>
            </template>
          </form.Field>
          <form.Field name="location">
            <template #default="{ field }">
              <Field :label="$ts('directory.profile.location')">
                <template #default="{ id }">
                  <Input
                    :id="id"
                    :model-value="field.state.value"
                    @update:model-value="field.handleChange($event)"
                  />
                </template>
              </Field>
            </template>
          </form.Field>
          <form.Field name="company">
            <template #default="{ field }">
              <Field :label="$ts('directory.search.company')">
                <template #default="{ id }">
                  <Input
                    :id="id"
                    :model-value="field.state.value"
                    @update:model-value="field.handleChange($event)"
                  />
                </template>
              </Field>
            </template>
          </form.Field>
        </div>

        <div class="flex flex-wrap items-center gap-3">
          <form.Field name="available">
            <template #default="{ field }">
              <div class="flex items-center gap-2 text-sm">
                <Checkbox
                  :id="availableId"
                  :model-value="field.state.value"
                  @update:model-value="field.handleChange($event)"
                />
                <label :for="availableId">{{ $t("directory.search.availableOnly") }}</label>
              </div>
            </template>
          </form.Field>
          <Button type="submit" class="ml-auto h-9" :disabled="loading">
            <Search class="size-3.5" />
            {{ $t("directory.search.search") }}
          </Button>
        </div>
      </form>

      <p
        v-if="error"
        class="rounded-md bg-destructive/10 px-3 py-2 text-sm text-destructive"
        role="alert"
      >
        {{ error }}
      </p>

      <section :aria-label="$ts('directory.search.results')" aria-live="polite">
        <div
          v-if="!loading && !error && !hits.length"
          class="rounded-lg border border-dashed py-12 text-center"
        >
          <SearchX class="mx-auto size-6 text-muted-foreground/60" />
          <p class="mt-2 text-sm font-medium">
            {{ filtered ? $t("directory.search.nobodyMatches") : $t("directory.search.empty") }}
          </p>
          <p class="mt-0.5 text-xs text-muted-foreground">
            {{
              filtered ? $t("directory.search.nobodyMatchesHint") : $t("directory.search.emptyHint")
            }}
          </p>
        </div>

        <ul class="space-y-2">
          <li v-for="hit in hits" :key="hit.username">
            <NuxtLink
              :to="
                localeRoute({
                  name: 'directory-people-username',
                  params: { username: hit.username },
                })
              "
              class="flex gap-3 rounded-lg border p-3 transition-colors hover:bg-accent"
            >
              <Avatar :name="hit.displayName || hit.username" />
              <span class="min-w-0 flex-1">
                <span class="flex flex-wrap items-center gap-2">
                  <span class="text-sm font-medium">{{ hit.displayName || hit.username }}</span>
                  <span
                    v-if="hit.available"
                    class="flex items-center gap-1 rounded-full bg-success/15 px-2 py-0.5 text-[0.6875rem]"
                  >
                    <BriefcaseBusiness class="size-3" />
                    {{ $t("directory.availability.available") }}
                  </span>
                </span>
                <span v-if="hit.headline" class="mt-0.5 block text-sm">{{ hit.headline }}</span>
                <span
                  v-if="hit.location"
                  class="mt-0.5 flex items-center gap-1 text-xs text-muted-foreground"
                >
                  <MapPin class="size-3" />
                  {{ hit.location }}
                </span>
                <span v-if="hit.skills?.length" class="mt-1.5 flex flex-wrap gap-1">
                  <span
                    v-for="skill in hit.skills"
                    :key="skill"
                    class="rounded-full border px-2 py-0.5 text-[0.6875rem] text-muted-foreground"
                  >
                    {{ skill }}
                  </span>
                </span>
              </span>
            </NuxtLink>
          </li>
        </ul>

        <div v-if="more" class="mt-4 flex justify-center">
          <Button variant="outline" :disabled="loading" @click="loadMore">
            <Spinner v-if="loading" size="sm" />
            {{ $t("directory.search.more") }}
          </Button>
        </div>
      </section>
    </div>
  </div>
</template>
