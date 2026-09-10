import * as Updater from "#bindings/updater";
import type { Settings, State } from "#bindings/updater/models";

/**
 * Whether there is a newer version, and what to do about it.
 *
 * Nothing here installs anything on its own. Checking says what exists,
 * downloading prepares it, and installing happens when somebody says so —
 * three steps rather than one, because replacing the application somebody is
 * using is their decision to make.
 */

/*
 * The shapes come from the generated bindings rather than being written again
 * here: they mirror the Go types, so a field that changes there stops this
 * compiling instead of quietly going missing at runtime.
 */
export type UpdateState = State;
export type UpdateSettings = Settings;

export function useUpdates() {
  const state = useState<UpdateState | null>("update-state", () => null);
  const settings = useState<UpdateSettings | null>("update-settings", () => null);
  const busy = useState("update-busy", () => false);

  async function refresh() {
    const [current, chosen] = await Promise.all([
      Updater.Service.UpdateState(),
      Updater.Service.UpdateSettings(),
    ]);

    state.value = current;
    settings.value = chosen;
  }

  /** Asks the feed now, whatever the schedule says. */
  async function check() {
    busy.value = true;
    try {
      state.value = await Updater.Service.CheckForUpdate();
    } finally {
      busy.value = false;
    }
  }

  /** Fetches and verifies, leaving it ready rather than installing it. */
  async function download() {
    busy.value = true;
    try {
      state.value = await Updater.Service.DownloadUpdate();
    } finally {
      busy.value = false;
    }
  }

  /**
   * Installs what was downloaded.
   *
   * The application has to be restarted afterwards — this replaces the file on
   * disk, and the version somebody is running is the one already in memory.
   */
  async function install() {
    busy.value = true;
    try {
      await Updater.Service.InstallUpdate();
      await refresh();
    } finally {
      busy.value = false;
    }
  }

  /** Declines it, and removes what was downloaded. */
  async function discard() {
    await Updater.Service.DiscardUpdate();
    await refresh();
  }

  async function save(next: UpdateSettings) {
    await Updater.Service.SetUpdateSettings(next);
    await refresh();
  }

  return { state, settings, busy, refresh, check, download, install, discard, save };
}
