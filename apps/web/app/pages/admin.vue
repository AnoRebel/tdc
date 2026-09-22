<script setup lang="ts">
import { isApiError } from "@tdc/types";
import { ArrowLeft, ShieldAlert, TriangleAlert } from "lucide-vue-next";
import { Switch } from "#layers/ui/app/components/ui/switch";
import { Select } from "#layers/ui/app/components/ui/select";
import { Input } from "#layers/ui/app/components/ui/input";
import { Textarea } from "#layers/ui/app/components/ui/textarea";

const { $t, $ts, $localeRoute: localeRoute } = useI18n();

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

useHead({ title: $ts("admin.serverSettings") });

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
  registration: $ts("admin.whoMayJoin"),
  limits: $ts("admin.capacity"),
  filter: $ts("admin.contentFilter"),
  server: $ts("admin.howThisServerIsFound"),
  federation: $ts("admin.otherServers"),
  brand: $ts("admin.howThisServerLooks"),
  audit: $ts("moderation.auditLog"),
};

/*
 * Settings whose values come from a fixed set get a menu rather than a text
 * box.
 *
 * The server validates either way, so this is not what makes a wrong value
 * impossible — it is what stops an operator discovering the valid values by
 * being refused. The lists match the server's own; a value it accepts that is
 * missing here would simply not be offered, which is a smaller failure than
 * offering one it refuses.
 */
const choices: Record<string, { value: string; label: string }[]> = {
  "registration.policy": [
    { value: "open", label: $ts("admin.anybodyMayRegister") },
    { value: "invitation", label: $ts("admin.onlyWithInvitation") },
    { value: "closed", label: $ts("admin.nobodyOperatorCreatesAccounts") },
    { value: "domain", label: $ts("admin.onlyTheseEmailDomains") },
  ],
  "server.visibility": [
    { value: "public", label: $ts("admin.anybodyMayFindThisServer") },
    { value: "limited", label: $ts("admin.onlyServersListedBelow") },
    { value: "private", label: $ts("admin.nobodyReachableOnlyByInvitation") },
  ],
  "federation.mode": [
    { value: "open", label: $ts("admin.anyServerThatProvesWhoItIs") },
    { value: "allowlist", label: $ts("admin.onlyTheServersListedBelow") },
    { value: "blocklist", label: $ts("admin.anyServerExceptThoseListed") },
  ],
  "filter.action": [
    { value: "flag", label: $ts("admin.deliverAndShowModerator") },
    { value: "mask", label: $ts("admin.deliverWithWordsReplaced") },
    { value: "block", label: $ts("admin.refuseTheMessage") },
  ],
};

/** Settings better typed into a box than a line. */
const multiline = new Set([
  "filter.words",
  "registration.domains",
  "server.visible_to",
  "federation.allowed_servers",
  "federation.blocked_servers",
]);

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
    error.value = isApiError(cause) ? cause.message : $ts("admin.changeNotAccepted");
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
    error.value = isApiError(cause) ? cause.message : $ts("admin.capabilityNotChanged");
  }
}

/** A limit of zero means none, which is what an operator needs to read. */
function limitLabel(limit: number): string {
  return limit === 0 ? $ts("admin.noLimit") : limit.toLocaleString();
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
      <NuxtLink
        :to="localeRoute({ name: 'settings' })"
        class="btn btn-ghost size-7 px-0"
        :aria-label="$ts('admin.backToSettings')"
      >
        <ArrowLeft class="size-4" />
      </NuxtLink>
      <h1 class="text-sm font-semibold">{{ $t("admin.serverSettings") }}</h1>
    </header>

    <div class="max-w-3xl space-y-8 px-4 py-6">
      <div
        v-if="admin.failed.value"
        class="flex gap-3 rounded-lg border border-destructive/30 bg-destructive/8 p-4"
        role="alert"
      >
        <ShieldAlert class="mt-0.5 size-4 shrink-0 text-destructive" />
        <div class="text-sm">
          <p class="font-medium text-destructive">{{ $t("admin.cannotConfigureServer") }}</p>
          <p class="mt-1 text-muted-foreground">
            {{ $t("admin.needsPermissionBefore") }}
            <code class="rounded bg-muted px-1 py-0.5 font-mono text-xs">server.configure</code>
            {{ $t("admin.needsPermissionAfter") }}
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
          <h2 class="text-sm font-semibold">{{ $t("admin.whatThisServerHolds") }}</h2>
          <dl class="mt-3 grid gap-3 sm:grid-cols-3">
            <div class="rounded-lg border bg-surface px-3 py-2.5">
              <dt class="text-xs text-muted-foreground">{{ $t("nav.people") }}</dt>
              <dd class="tabular mt-0.5 text-sm">
                {{ admin.usage.value.users.used.toLocaleString() }}
                <span class="text-xs text-muted-foreground">
                  {{ $t("admin.ofLimit", { limit: limitLabel(admin.usage.value.users.limit) }) }}
                </span>
              </dd>
            </div>
            <div class="rounded-lg border bg-surface px-3 py-2.5">
              <dt class="text-xs text-muted-foreground">{{ $t("nav.channels") }}</dt>
              <dd class="tabular mt-0.5 text-sm">
                {{ admin.usage.value.channels.used.toLocaleString() }}
                <span class="text-xs text-muted-foreground">
                  {{ $t("admin.ofLimit", { limit: limitLabel(admin.usage.value.channels.limit) }) }}
                </span>
              </dd>
            </div>
            <div class="rounded-lg border bg-surface px-3 py-2.5">
              <dt class="text-xs text-muted-foreground">{{ $t("attachment.attachments") }}</dt>
              <dd class="tabular mt-0.5 text-sm">
                {{ bytes(admin.usage.value.storage.used) }}
                <span class="text-xs text-muted-foreground">
                  {{
                    $t("admin.ofLimit", {
                      limit:
                        admin.usage.value.storage.limit === 0
                          ? $ts("admin.noLimit")
                          : bytes(admin.usage.value.storage.limit),
                    })
                  }}
                </span>
              </dd>
            </div>
          </dl>
        </section>

        <section>
          <h2 class="text-sm font-semibold">{{ $t("admin.whatThisServerOffers") }}</h2>
          <p class="mt-0.5 text-xs text-muted-foreground">
            {{ $t("admin.capabilityOffHint") }}
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
                    {{ $t("admin.changed") }}
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
                  {{ $t("admin.notConfiguredYetHint") }}
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

        <!-- Unsafe-but-valid settings, said plainly and near the top: an
             operator who has to scroll past the whole configuration to find
             out their server exposes internal errors will not find out. -->
        <section v-if="admin.warnings.value.length">
          <h2 class="flex items-center gap-1.5 text-sm font-semibold text-warning">
            <TriangleAlert class="size-4" />
            {{ $t("admin.worthChecking") }}
          </h2>
          <ul class="mt-3 space-y-1.5">
            <li
              v-for="warning in admin.warnings.value"
              :key="warning"
              class="rounded-lg border border-warning/30 bg-warning/8 px-3 py-2 text-sm"
            >
              {{ warning }}
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
              <Select
                v-if="choices[setting.key]"
                :id="setting.key"
                :model-value="String(setting.value)"
                class="mt-1.5"
                :label="settingLabel(setting.key)"
                :disabled="saving === setting.key"
                :options="
                  (choices[setting.key] ?? []).map((choice) => ({
                    value: String(choice.value),
                    label: choice.label,
                  }))
                "
                @update:model-value="$event !== undefined && save(setting.key, $event)"
              />

              <Textarea
                class="mt-1.5 h-auto py-2"
                v-else-if="multiline.has(setting.key)"
                :id="setting.key"
                :value="setting.value"
                rows="3"
                :disabled="saving === setting.key"
                @change="save(setting.key, ($event.target as HTMLTextAreaElement).value)"
              />

              <Input
                class="mt-1.5"
                v-else
                :id="setting.key"
                :value="setting.value"
                :disabled="saving === setting.key"
                @change="save(setting.key, ($event.target as HTMLInputElement).value)"
              />
            </div>
          </div>
        </section>

        <section v-if="admin.configuration.value.length">
          <h2 class="text-sm font-semibold">{{ $t("admin.howThisServerWasStarted") }}</h2>
          <p class="mt-0.5 text-xs text-muted-foreground">
            {{ $t("admin.readOnlyConfigurationHint") }}
          </p>

          <div class="mt-3 overflow-x-auto rounded-lg border bg-surface">
            <table class="w-full text-sm">
              <thead>
                <tr class="border-b text-left">
                  <th scope="col" class="px-3 py-2 text-xs font-medium text-muted-foreground">
                    {{ $t("admin.setting") }}
                  </th>
                  <th scope="col" class="px-3 py-2 text-xs font-medium text-muted-foreground">
                    {{ $t("admin.value") }}
                  </th>
                  <th scope="col" class="px-3 py-2 text-xs font-medium text-muted-foreground">
                    {{ $t("admin.from") }}
                  </th>
                </tr>
              </thead>
              <tbody>
                <tr
                  v-for="value in admin.configuration.value"
                  :key="value.key"
                  class="border-b transition last:border-0 hover:bg-surface-sunken"
                >
                  <td class="px-3 py-2 font-mono text-xs whitespace-nowrap">{{ value.key }}</td>
                  <td class="px-3 py-2">
                    <span v-if="value.value" class="font-mono text-xs break-all">
                      {{ value.value }}
                    </span>
                    <span v-else class="text-xs text-muted-foreground">{{
                      $t("admin.notSet")
                    }}</span>
                    <span
                      v-if="value.secret"
                      class="ml-1.5 rounded bg-muted px-1.5 py-0.5 text-[0.6875rem] text-muted-foreground"
                    >
                      {{ $t("admin.redacted") }}
                    </span>
                  </td>
                  <td class="px-3 py-2">
                    <!-- A defaulted value is the quiet case and reads as such;
                         one that was actually chosen stands out, which is what
                         somebody scanning this column is looking for. -->
                    <span
                      class="text-xs"
                      :class="
                        value.source === 'default' ? 'text-muted-foreground' : 'text-foreground'
                      "
                    >
                      {{ value.source }}
                    </span>
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
