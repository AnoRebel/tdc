<script setup lang="ts">
import { isApiError } from "@tdc/types";
import { ArrowLeft, ShieldAlert, TriangleAlert } from "lucide-vue-next";
import { Switch } from "~/components/ui/switch";

/**
 * Server administration.
 *
 * Everything an operator would otherwise reach for curl to do: registration
 * policy, capacity limits, and which capabilities this server offers at all.
 *
 * Reaching this page needs the configure permission, which the server checks
 * on every request. The check below only decides what to show — somebody
 * arriving without it sees the refusal rather than an empty page, because an
 * empty page and a refused one look the same and mean opposite things.
 */

definePageMeta({ middleware: "auth", layout: "app" });

useHead({ title: "Server settings" });

const admin = useServerAdmin();

const error = ref<string | null>(null);
const saving = ref<string | null>(null);

onMounted(() => void admin.refresh());

/*
 * Settings are grouped by the prefix of their key.
 *
 * The server names them registration.policy, limits.max_users and so on, which
 * is already the grouping an operator thinks in. Deriving the sections from
 * that rather than listing them here means a new setting appears in the right
 * place without this file changing.
 */
const groups = computed(() => {
  const byPrefix = new Map<string, typeof admin.settings.value>();

  for (const setting of admin.settings.value) {
    const prefix = setting.key.split(".")[0] ?? "other";
    const existing = byPrefix.get(prefix) ?? [];
    existing.push(setting);
    byPrefix.set(prefix, existing);
  }

  return [...byPrefix.entries()].map(([prefix, settings]) => ({
    prefix,
    title: groupTitles[prefix] ?? prefix,
    settings: settings.sort((a, b) => a.key.localeCompare(b.key)),
  }));
});

const groupTitles: Record<string, string> = {
  registration: "Who may join",
  limits: "Capacity",
};

/** The last segment of a key, as a label. */
function settingLabel(key: string): string {
  const name = key.split(".").slice(1).join(".").replace(/_/g, " ");
  return name.charAt(0).toUpperCase() + name.slice(1);
}

async function save(key: string, value: string) {
  saving.value = key;
  error.value = null;
  try {
    await admin.setSetting(key, value);
  } catch (cause) {
    error.value = isApiError(cause) ? cause.message : "That change was not accepted.";
    await admin.refresh();
  } finally {
    saving.value = null;
  }
}

async function toggle(name: string, enabled: boolean) {
  error.value = null;
  try {
    await admin.setFeature(name, enabled);
  } catch (cause) {
    error.value = isApiError(cause) ? cause.message : "That capability could not be changed.";
  }
}

/** A limit of zero means none, which is what an operator needs to read. */
function limitLabel(limit: number): string {
  return limit === 0 ? "no limit" : limit.toLocaleString();
}

function bytes(value: number): string {
  if (value < 1024) return `${value} B`;
  const units = ["KB", "MB", "GB", "TB"];
  let size = value / 1024;
  let unit = 0;
  while (size >= 1024 && unit < units.length - 1) {
    size /= 1024;
    unit += 1;
  }
  return `${size.toFixed(size < 10 ? 1 : 0)} ${units[unit]}`;
}
</script>

<template>
  <div class="flex-1 overflow-y-auto">
    <header class="flex h-12 shrink-0 items-center gap-3 border-b px-4">
      <NuxtLink to="/settings" class="btn btn-ghost size-7 px-0" aria-label="Back to settings">
        <ArrowLeft class="size-4" />
      </NuxtLink>
      <h1 class="text-sm font-semibold">Server settings</h1>
    </header>

    <div class="max-w-3xl space-y-8 px-4 py-6">
      <div
        v-if="admin.failed.value"
        class="flex gap-3 rounded-lg border border-destructive/30 bg-destructive/8 p-4"
        role="alert"
      >
        <ShieldAlert class="mt-0.5 size-4 shrink-0 text-destructive" />
        <div class="text-sm">
          <p class="font-medium text-destructive">You cannot configure this server.</p>
          <p class="mt-1 text-muted-foreground">
            This needs the
            <code class="rounded bg-muted px-1 py-0.5 font-mono text-xs">server.configure</code>
            permission.
          </p>
        </div>
      </div>

      <template v-else>
        <p
          v-if="error"
          class="rounded-md bg-destructive/10 px-3 py-2 text-sm text-destructive"
          role="alert"
        >
          {{ error }}
        </p>

        <section v-if="admin.usage.value">
          <h2 class="text-sm font-semibold">What this server holds</h2>
          <dl class="mt-3 grid gap-3 sm:grid-cols-3">
            <div class="rounded-lg border bg-surface px-3 py-2.5">
              <dt class="text-xs text-muted-foreground">People</dt>
              <dd class="tabular mt-0.5 text-sm">
                {{ admin.usage.value.users.used.toLocaleString() }}
                <span class="text-xs text-muted-foreground">
                  of {{ limitLabel(admin.usage.value.users.limit) }}
                </span>
              </dd>
            </div>
            <div class="rounded-lg border bg-surface px-3 py-2.5">
              <dt class="text-xs text-muted-foreground">Channels</dt>
              <dd class="tabular mt-0.5 text-sm">
                {{ admin.usage.value.channels.used.toLocaleString() }}
                <span class="text-xs text-muted-foreground">
                  of {{ limitLabel(admin.usage.value.channels.limit) }}
                </span>
              </dd>
            </div>
            <div class="rounded-lg border bg-surface px-3 py-2.5">
              <dt class="text-xs text-muted-foreground">Attachments</dt>
              <dd class="tabular mt-0.5 text-sm">
                {{ bytes(admin.usage.value.storage.used) }}
                <span class="text-xs text-muted-foreground">
                  of
                  {{
                    admin.usage.value.storage.limit === 0
                      ? "no limit"
                      : bytes(admin.usage.value.storage.limit)
                  }}
                </span>
              </dd>
            </div>
          </dl>
        </section>

        <section>
          <h2 class="text-sm font-semibold">What this server offers</h2>
          <p class="mt-0.5 text-xs text-muted-foreground">
            A capability turned off here is refused by the server and is not advertised to clients,
            so the control disappears rather than failing.
          </p>

          <ul class="mt-3 space-y-1">
            <li
              v-for="feature in admin.features.value"
              :key="feature.name"
              class="flex items-start justify-between gap-4 rounded-lg px-2 py-2.5 transition-colors hover:bg-accent"
            >
              <div class="min-w-0">
                <p class="text-sm font-medium">
                  {{ feature.name }}
                  <span
                    v-if="feature.enabled !== feature.default"
                    class="ml-1.5 rounded bg-primary/15 px-1.5 py-0.5 text-[0.6875rem] font-medium text-primary"
                  >
                    changed
                  </span>
                </p>
                <p class="mt-0.5 text-xs text-muted-foreground">{{ feature.description }}</p>

                <!-- Switched on but unable to work. Saying so here is the
                     difference between a setting that looks applied and one
                     that is. -->
                <p
                  v-if="feature.enabled && !feature.available"
                  class="mt-1 flex items-center gap-1 text-xs text-warning"
                >
                  <TriangleAlert class="size-3 shrink-0" />
                  Nothing is configured for this yet, so it stays unavailable.
                </p>
              </div>

              <Switch
                :model-value="feature.enabled"
                :label="feature.description"
                @update:model-value="(on: boolean) => toggle(feature.name, on)"
              />
            </li>
          </ul>
        </section>

        <section v-for="group in groups" :key="group.prefix">
          <h2 class="text-sm font-semibold">{{ group.title }}</h2>

          <div class="mt-3 space-y-3">
            <div v-for="setting in group.settings" :key="setting.key">
              <label :for="setting.key" class="block text-sm font-medium">
                {{ settingLabel(setting.key) }}
              </label>
              <p class="mt-0.5 text-xs text-muted-foreground">{{ setting.description }}</p>
              <input
                :id="setting.key"
                :value="setting.value"
                class="input mt-1.5"
                :disabled="saving === setting.key"
                @change="save(setting.key, ($event.target as HTMLInputElement).value)"
              />
            </div>
          </div>
        </section>
      </template>
    </div>
  </div>
</template>
