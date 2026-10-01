<script setup lang="ts">
import type { Enquiry } from "@tdc/types";
import { isApiError } from "@tdc/types";
import { ArrowLeft, Inbox, MessagesSquare } from "lucide-vue-next";
import { whenBriefly } from "#layers/ui/app/lib/dates";

const { $t, $ts, $localeRoute: localeRoute } = useI18n();

/**
 * Enquiries: the ones somebody received through their profile, and the ones
 * they sent.
 *
 * A received enquiry opens its conversation, where it is the first message.
 * A sent one shows where it stands; the sender is not in the conversation
 * until the recipient takes it up.
 */

definePageMeta({ middleware: "auth", layout: "app" });

useHead({ title: $ts("directory.enquiry.title") });

const route = useRoute();
const router = useRouter();
const directory = useDirectory();

const box = computed<"received" | "sent">(() => (route.query.box === "sent" ? "sent" : "received"));
const enquiries = ref<Enquiry[]>([]);
const error = ref<string | null>(null);
const loading = ref(true);

async function load() {
  loading.value = true;
  error.value = null;
  try {
    enquiries.value = await directory.enquiries(box.value);
  } catch (cause) {
    error.value = isApiError(cause) ? cause.message : $ts("directory.couldNotReach");
  } finally {
    loading.value = false;
  }
}

await load();
watch(box, load);

function choose(which: "received" | "sent") {
  void router.replace({ query: which === "sent" ? { box: "sent" } : {} });
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
      <h1 class="text-sm font-semibold">{{ $t("directory.enquiry.title") }}</h1>
    </header>

    <div class="max-w-3xl space-y-4 px-4 py-6">
      <div role="tablist" class="inline-flex rounded-lg border p-0.5">
        <button
          v-for="which in ['received', 'sent'] as const"
          :key="which"
          type="button"
          role="tab"
          :aria-selected="box === which"
          class="rounded-md px-3 py-1 text-sm aria-selected:bg-accent aria-selected:font-medium"
          @click="choose(which)"
        >
          {{ which === "sent" ? $t("directory.enquiry.sent") : $t("directory.enquiry.received") }}
        </button>
      </div>

      <p
        v-if="error"
        class="rounded-md bg-destructive/10 px-3 py-2 text-sm text-destructive"
        role="alert"
      >
        {{ error }}
      </p>

      <div
        v-if="!loading && !error && !enquiries.length"
        class="rounded-lg border border-dashed py-12 text-center"
      >
        <Inbox class="mx-auto size-6 text-muted-foreground/60" />
        <p class="mt-2 text-sm font-medium">{{ $t("directory.enquiry.none") }}</p>
      </div>

      <ul class="space-y-2">
        <li v-for="enquiry in enquiries" :key="enquiry.id" class="rounded-lg border p-3">
          <div class="flex flex-wrap items-center gap-2">
            <span class="text-sm font-medium">{{ enquiry.subject }}</span>
            <span
              class="rounded-full bg-muted px-2 py-0.5 text-[0.6875rem] text-muted-foreground"
              :data-status="enquiry.status"
            >
              {{ $t(`directory.enquiry.status.${enquiry.status}`) }}
            </span>
            <time class="ml-auto text-xs text-muted-foreground" :datetime="enquiry.createdAt">
              {{ whenBriefly(enquiry.createdAt) }}
            </time>
          </div>
          <p class="mt-0.5 text-xs text-muted-foreground">
            {{
              enquiry.received
                ? $t("directory.enquiry.from", { name: enquiry.withName || enquiry.with })
                : $t("directory.enquiry.to", { name: enquiry.withName || enquiry.with })
            }}
          </p>
          <p class="mt-2 line-clamp-3 text-sm whitespace-pre-wrap">{{ enquiry.body }}</p>
          <NuxtLink
            v-if="enquiry.received && enquiry.roomId"
            :to="`/rooms/${enquiry.roomId}`"
            class="btn btn-ghost mt-2"
          >
            <MessagesSquare class="size-3.5" />
            {{ $t("directory.enquiry.open") }}
          </NuxtLink>
        </li>
      </ul>
    </div>
  </div>
</template>
