import type { FeatureToggle } from "@tdc/types";

/** One server setting as an administrator sees it. */
export interface ServerSetting {
  key: string;
  value: string;
  description: string;
  fallback: string;
}

/** What a server currently holds against what it will allow. */
export interface Usage {
  users: { used: number; limit: number };
  channels: { used: number; limit: number };
  storage: { used: number; limit: number };
}

/**
 * Server administration: policy, capacity and capability toggles.
 *
 * Everything here needs the configure permission, which most people do not
 * have. A refusal is therefore expected rather than exceptional, and is
 * reported as such rather than as an error — the page says you cannot see this
 * instead of implying something broke.
 */
export function useServerAdmin() {
  const api = useApi();

  const settings = ref<ServerSetting[]>([]);
  const features = ref<FeatureToggle[]>([]);
  const usage = ref<Usage | null>(null);
  const failed = ref(false);
  const loading = ref(false);

  async function refresh() {
    loading.value = true;
    try {
      const [loadedSettings, loadedFeatures, loadedUsage] = await Promise.all([
        api.get<ServerSetting[]>("/api/admin/settings"),
        api.get<FeatureToggle[]>("/api/admin/features"),
        api.get<Usage>("/api/admin/usage"),
      ]);
      settings.value = loadedSettings;
      features.value = loadedFeatures;
      usage.value = loadedUsage;
      failed.value = false;
    } catch {
      settings.value = [];
      features.value = [];
      usage.value = null;
      failed.value = true;
    } finally {
      loading.value = false;
    }
  }

  /**
   * Changes a setting.
   *
   * The value is not applied locally until the server has accepted it: a
   * settings page showing a value the server refused is a page that lies about
   * what the server will do.
   */
  async function setSetting(key: string, value: string) {
    await api.put(`/api/admin/settings/${key}`, { value });
    await refresh();
  }

  /**
   * Turns a capability on or off.
   *
   * Applied optimistically and put back on failure, because a switch that does
   * not move until a round trip completes feels broken — but a switch showing
   * a state the server refused is worse, so the failure path is explicit.
   */
  async function setFeature(name: string, enabled: boolean) {
    const toggle = features.value.find((feature) => feature.name === name);
    const before = toggle?.enabled;
    if (toggle) toggle.enabled = enabled;

    try {
      await api.put(`/api/admin/features/${name}`, { enabled });
    } catch (cause) {
      if (toggle && before !== undefined) toggle.enabled = before;
      throw cause;
    }
  }

  return { settings, features, usage, failed, loading, refresh, setSetting, setFeature };
}
