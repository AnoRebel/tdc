<script setup lang="ts">
import { BookOpen, CircleAlert, FileJson, Filter } from "lucide-vue-next";
/**
 * The API, as this server describes it.
 *
 * Read from GET /api/description, which the server generates from its own
 * routing table — so this lists what the server actually serves, version for
 * version, rather than what somebody once wrote down. Grouped by the first
 * segment of the path, which is how the API is organised: rooms, projects,
 * directory, admin.
 *
 * What a routing table cannot honestly say — the shape of a request or a
 * response — it does not pretend to. The Integrating guide is where that is.
 */
import { Button } from "#layers/ui/app/components/ui/button";
import { Input } from "#layers/ui/app/components/ui/input";

definePageMeta({ middleware: "auth", layout: "app" });

const { $t, $ts, $localeRoute: localeRoute } = useI18n();

useHead({ title: $ts("help.apiReference.title") });

interface Endpoint {
  method: string;
  path: string;
  parameters?: string[];
  summary: string;
  authentication: string;
  permission?: string;
}

interface Description {
  server: string;
  version: string;
  endpoints: Endpoint[];
}

const api = useApi();
// The OpenAPI document is on the server, which is elsewhere in the desktop app.
const mediaBase = useMediaBase();
const description = ref<Description | null>(null);
const failed = ref(false);
const filter = ref("");

async function load() {
  failed.value = false;
  try {
    description.value = await api.get<Description>("/api/description");
  } catch {
    failed.value = true;
  }
}

void load();

/** The area an endpoint belongs to: /api/rooms/:id/messages is "rooms". */
function areaOf(path: string): string {
  return path.replace(/^\/api\//, "").split("/")[0] || "api";
}

const areas = computed(() => {
  const text = filter.value.trim().toLowerCase();
  const grouped = new Map<string, Endpoint[]>();

  for (const endpoint of description.value?.endpoints ?? []) {
    const haystack =
      `${endpoint.method} ${endpoint.path} ${endpoint.summary} ${endpoint.permission ?? ""}`.toLowerCase();
    if (text && !text.split(/\s+/).every((word) => haystack.includes(word))) continue;

    const area = areaOf(endpoint.path);
    grouped.set(area, [...(grouped.get(area) ?? []), endpoint]);
  }

  return [...grouped.entries()].sort(([a], [b]) => a.localeCompare(b));
});

const shown = computed(() =>
  areas.value.reduce((total, [, endpoints]) => total + endpoints.length, 0),
);

/** The method's colour, so a destructive call stands out in a long list. */
function methodClass(method: string): string {
  switch (method) {
    case "GET":
      return "bg-primary/10 text-primary";
    case "POST":
      return "bg-success/15 text-success-text";
    case "DELETE":
      return "bg-destructive/10 text-destructive";
    default:
      return "bg-warning/15 text-foreground";
  }
}
</script>

<template>
  <div class="flex-1 overflow-y-auto">
    <HelpHeader :current="$ts('help.apiReference.title')">
      <NuxtLink
        :to="localeRoute({ name: 'help-slug', params: { slug: 'integrating' } })"
        class="btn btn-ghost h-7 gap-1.5 px-2 text-xs text-muted-foreground"
      >
        <BookOpen class="size-3.5" aria-hidden="true" />
        <span class="max-sm:sr-only">{{ $t("help.api.openGuide") }}</span>
      </NuxtLink>
      <a
        :href="`${mediaBase}/api/openapi.json`"
        target="_blank"
        rel="noopener"
        class="btn btn-ghost h-7 gap-1.5 px-2 text-xs text-muted-foreground"
      >
        <FileJson class="size-3.5" aria-hidden="true" />
        <span class="max-sm:sr-only">{{ $t("help.api.openapi") }}</span>
      </a>
    </HelpHeader>

    <div class="mx-auto max-w-6xl px-4 pt-8 pb-24 md:px-8">
      <header class="mb-6">
        <h2 class="text-2xl font-semibold tracking-tight">{{ $t("help.apiReference.title") }}</h2>
        <p class="mt-1.5 max-w-2xl text-sm text-muted-foreground">{{ $t("help.api.intro") }}</p>
        <p v-if="description" class="mt-2 font-mono text-xs text-muted-foreground">
          {{ description.server }} · {{ description.version }}
        </p>
      </header>

      <div
        v-if="failed"
        class="flex items-center gap-3 rounded-lg border px-4 py-3 text-sm"
        role="alert"
      >
        <CircleAlert class="size-4 text-destructive" aria-hidden="true" />
        <span class="flex-1">{{ $t("help.api.loadFailed") }}</span>
        <Button variant="outline" size="small" @click="load">{{ $t("help.retry") }}</Button>
      </div>

      <div v-else-if="!description" class="space-y-2" aria-hidden="true">
        <div v-for="row in 10" :key="row" class="h-9 animate-pulse rounded-md bg-muted" />
      </div>

      <template v-else>
        <div
          class="sticky top-12 z-[5] -mx-1 mb-6 flex items-center gap-3 bg-background/90 px-1 py-2 backdrop-blur"
        >
          <label class="relative block max-w-sm flex-1">
            <span class="sr-only">{{ $t("help.api.filter") }}</span>
            <Filter
              class="pointer-events-none absolute top-1/2 z-10 left-2.5 size-3.5 -translate-y-1/2 text-muted-foreground"
              aria-hidden="true"
            />
            <Input
              v-model="filter"
              type="search"
              :placeholder="$ts('help.api.filter')"
              class="pl-8"
            />
          </label>
          <span class="tabular text-xs text-muted-foreground">
            {{ $t("help.api.count", { count: shown }) }}
          </span>
        </div>

        <p v-if="!areas.length" class="text-sm text-muted-foreground">
          {{ $t("help.api.none", { query: filter.trim() }) }}
        </p>

        <section v-for="[area, endpoints] in areas" :key="area" class="mb-8">
          <h3 class="mb-2 flex items-center gap-2 font-mono text-sm font-semibold">
            /api/{{ area }}
            <span
              class="tabular rounded-full bg-muted px-1.5 font-sans text-[0.6875rem] font-medium text-muted-foreground"
            >
              {{ endpoints.length }}
            </span>
          </h3>
          <div class="overflow-x-auto rounded-lg border">
            <table class="w-full text-sm">
              <thead class="bg-muted/50 text-left text-xs text-muted-foreground">
                <tr>
                  <th scope="col" class="w-20 px-3 py-2 font-medium">
                    {{ $t("help.api.method") }}
                  </th>
                  <th scope="col" class="px-3 py-2 font-medium">{{ $t("help.api.path") }}</th>
                  <th scope="col" class="px-3 py-2 font-medium">{{ $t("help.api.summary") }}</th>
                  <th scope="col" class="px-3 py-2 font-medium">
                    {{ $t("help.api.authentication") }}
                  </th>
                  <th scope="col" class="px-3 py-2 font-medium">{{ $t("help.api.permission") }}</th>
                </tr>
              </thead>
              <tbody class="divide-y">
                <tr
                  v-for="endpoint in endpoints"
                  :key="`${endpoint.method} ${endpoint.path}`"
                  class="hover:bg-muted/30"
                >
                  <td class="px-3 py-2 align-top">
                    <span
                      class="inline-block rounded px-1.5 py-0.5 font-mono text-[0.6875rem] font-semibold"
                      :class="methodClass(endpoint.method)"
                    >
                      {{ endpoint.method }}
                    </span>
                  </td>
                  <td class="px-3 py-2 align-top font-mono text-[0.8125rem] whitespace-nowrap">
                    {{ endpoint.path }}
                  </td>
                  <td class="px-3 py-2 align-top">{{ endpoint.summary }}</td>
                  <td class="px-3 py-2 align-top text-xs whitespace-nowrap text-muted-foreground">
                    {{ endpoint.authentication }}
                  </td>
                  <td class="px-3 py-2 align-top font-mono text-xs text-muted-foreground">
                    {{ endpoint.permission ?? "—" }}
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </section>
      </template>
    </div>
  </div>
</template>
