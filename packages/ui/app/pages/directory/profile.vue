<script setup lang="ts">
import type { CompanyAssociation, OwnProfile, ProfileDocument } from "@tdc/types";
import { isApiError } from "@tdc/types";
import { ArrowLeft, Building2, Download, Eye, Plus, Trash2 } from "lucide-vue-next";
import { Button } from "#layers/ui/app/components/ui/button";
import { onDate } from "#layers/ui/app/lib/dates";

const { $t, $ts, $localeRoute: localeRoute } = useI18n();

/**
 * Somebody's own directory profile: writing it, taking it away, and the
 * companies that have asked to list them.
 *
 * Having no profile is a complete state, not an error. The page says what a
 * profile is for and offers to make one, and nothing else about the account
 * depends on the answer.
 */

definePageMeta({ middleware: "auth", layout: "app" });

useHead({ title: $ts("directory.profile.title") });

const directory = useDirectory();

const own = ref<OwnProfile | null>(null);
const associations = ref<CompanyAssociation[]>([]);
const loaded = ref(false);
const error = ref<string | null>(null);
const notice = ref<string | null>(null);
const deleting = ref(false);
// Bumped after a save so the editor starts again from what the server kept.
const editorKey = ref(0);

async function load() {
  try {
    const [profile, companies] = await Promise.all([
      directory.ownProfile(),
      directory.myCompanies(),
    ]);
    own.value = profile;
    associations.value = companies;
  } catch (cause) {
    error.value = isApiError(cause) ? cause.message : $ts("directory.couldNotReach");
  } finally {
    loaded.value = true;
  }
}

await load();

const initial = computed<ProfileDocument>(() => own.value?.profile ?? emptyProfile());

/** What each of the owner's verifications is about, in words. */
function claimLabel(kind: string, claimId: string): string {
  const profile = own.value?.profile;
  if (kind === "identity") return $ts("directory.verification.identity");
  if (kind === "skill") return profile?.skills.find((skill) => skill.id === claimId)?.name ?? "";
  if (kind === "portfolio")
    return profile?.portfolio.find((item) => item.id === claimId)?.title ?? "";
  return $ts("directory.verification.company");
}

async function saved() {
  const wasCreating = !own.value;
  await load();
  editorKey.value++;
  notice.value = wasCreating ? $ts("directory.profile.created") : $ts("directory.profile.saved");
}

async function confirmDelete() {
  deleting.value = false;
  try {
    await directory.deleteProfile();
    own.value = null;
    editorKey.value++;
    notice.value = $ts("directory.profile.deleted");
  } catch (cause) {
    error.value = isApiError(cause) ? cause.message : $ts("directory.couldNotReach");
  }
}

/*
 * The export, handed over as a file.
 *
 * Built in the page from what the server returned rather than by navigating to
 * the endpoint, because the request carries a token the browser would not send
 * on a plain navigation — and the desktop client has no navigation at all.
 */
async function exportProfile() {
  try {
    const document = await directory.exportProfile();
    const blob = new Blob([JSON.stringify(document, null, 2)], { type: "application/json" });
    const link = window.document.createElement("a");
    link.href = URL.createObjectURL(blob);
    link.download = `directory-profile-${own.value?.username ?? "export"}.json`;
    link.click();
    URL.revokeObjectURL(link.href);
  } catch (cause) {
    error.value = isApiError(cause) ? cause.message : $ts("directory.couldNotReach");
  }
}

async function accept(association: CompanyAssociation) {
  try {
    await directory.acceptCompany(association.slug);
    await load();
  } catch (cause) {
    error.value = isApiError(cause) ? cause.message : $ts("directory.couldNotReach");
  }
}

async function leave(association: CompanyAssociation) {
  try {
    await directory.leaveCompany(association.slug);
    await load();
  } catch (cause) {
    error.value = isApiError(cause) ? cause.message : $ts("directory.couldNotReach");
  }
}
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
      <h1 class="text-sm font-semibold">{{ $t("directory.profile.title") }}</h1>
    </header>

    <div class="max-w-3xl space-y-8 px-4 py-6">
      <p
        v-if="error"
        class="rounded-md bg-destructive/10 px-3 py-2 text-sm text-destructive"
        role="alert"
      >
        {{ error }}
      </p>
      <p
        v-if="notice"
        class="rounded-md bg-primary/10 px-3 py-2 text-sm text-foreground"
        role="status"
      >
        {{ notice }}
      </p>

      <!-- Why somebody would, stated before they are asked to. -->
      <section v-if="loaded && !own" class="rounded-lg border border-dashed p-4">
        <h2 class="text-sm font-semibold">{{ $t("directory.profile.noneYet") }}</h2>
        <p class="mt-1 text-sm text-muted-foreground">{{ $t("directory.profile.noneYetHint") }}</p>
      </section>

      <div v-if="own" class="flex flex-wrap gap-2">
        <NuxtLink
          :to="
            localeRoute({ name: 'directory-people-username', params: { username: own.username } })
          "
          class="btn btn-outline"
        >
          <Eye class="size-3.5" />
          {{ $t("directory.profile.seeAsOthers") }}
        </NuxtLink>
        <Button variant="outline" @click="exportProfile">
          <Download class="size-3.5" />
          {{ $t("directory.profile.export") }}
        </Button>
        <Button variant="danger" class="ml-auto" @click="deleting = true">
          <Trash2 class="size-3.5" />
          {{ $t("directory.profile.delete") }}
        </Button>
      </div>

      <section v-if="own?.verifications.length" class="space-y-2 rounded-lg border p-3">
        <h2 class="text-sm font-semibold">{{ $t("directory.verification.yours") }}</h2>
        <p class="text-xs text-muted-foreground">{{ $t("directory.verification.editClears") }}</p>
        <ul class="space-y-2">
          <li
            v-for="verification in own.verifications"
            :key="verification.kind + verification.claimId"
            class="flex flex-wrap items-start gap-2 text-sm"
          >
            <span class="font-medium">{{
              claimLabel(verification.kind, verification.claimId)
            }}</span>
            <DirectoryVerifiedBadge :verified="verification" />
          </li>
        </ul>
      </section>

      <DirectoryProfileEditor
        v-if="loaded"
        :key="editorKey"
        :initial="initial"
        :creating="!own"
        @saved="saved"
      />

      <section class="space-y-3 border-t pt-6">
        <div class="flex items-center gap-2">
          <h2 class="text-sm font-semibold">{{ $t("directory.company.yourCompanies") }}</h2>
          <NuxtLink
            :to="localeRoute({ name: 'directory-companies-new' })"
            class="btn btn-ghost ml-auto"
          >
            <Plus class="size-3.5" />
            {{ $t("directory.company.create") }}
          </NuxtLink>
        </div>
        <p v-if="!associations.length" class="text-xs text-muted-foreground">
          {{ $t("directory.company.noneYet") }}
        </p>
        <ul class="space-y-2">
          <li
            v-for="association in associations"
            :key="association.slug"
            class="flex flex-wrap items-center gap-3 rounded-lg border p-3"
          >
            <Building2 class="size-4 shrink-0 text-muted-foreground" />
            <div class="min-w-0 flex-1">
              <NuxtLink
                :to="
                  localeRoute({
                    name: 'directory-companies-slug',
                    params: { slug: association.slug },
                  })
                "
                class="text-sm font-medium hover:underline"
              >
                {{ association.name }}
              </NuxtLink>
              <p class="text-xs text-muted-foreground">
                <template v-if="association.pending">
                  {{ $t("directory.company.askedYou", { date: onDate(association.invitedAt) }) }}
                </template>
                <template v-else>
                  {{ association.role || $t("directory.company.member") }}
                  <template v-if="association.administrator">
                    · {{ $t("directory.company.administrator") }}
                  </template>
                </template>
              </p>
            </div>
            <template v-if="association.pending">
              <Button @click="accept(association)">{{ $t("directory.company.accept") }}</Button>
              <Button variant="ghost" @click="leave(association)">
                {{ $t("directory.company.decline") }}
              </Button>
            </template>
            <Button v-else variant="ghost" @click="leave(association)">
              {{ $t("directory.company.leave") }}
            </Button>
          </li>
        </ul>
        <p v-if="associations.some((a) => a.pending)" class="text-xs text-muted-foreground">
          {{ $t("directory.company.acceptMeans") }}
        </p>
      </section>
    </div>

    <ConfirmDialog
      v-if="deleting"
      :title="$ts('directory.profile.deleteTitle')"
      :detail="$ts('directory.profile.deleteDetail')"
      :confirm-label="$ts('directory.profile.delete')"
      @cancel="deleting = false"
      @confirm="confirmDelete"
    />
  </div>
</template>
