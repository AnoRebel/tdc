import { Events } from "@wailsio/runtime";

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

  /*
   * What the download is doing, from Wails' updater's own events: how much
   * has arrived, then checking the signature, then putting it in place.
   * Null when nothing is under way.
   */
  const step = useState<"downloading" | "verifying" | "installing" | null>(
    "update-step",
    () => null,
  );
  const progress = useState<{ written: number; total: number; rate: number } | null>(
    "update-progress",
    () => null,
  );
  const payload = (event: { data?: unknown }) =>
    (Array.isArray(event.data) ? event.data[0] : event.data) as
      { written?: number; total?: number; rate?: number } | undefined;
  const offs = [
    Events.On("wails:updater:download-started", () => {
      step.value = "downloading";
      progress.value = null;
    }),
    Events.On("wails:updater:download-progress", (event: { data?: unknown }) => {
      const data = payload(event);
      step.value = "downloading";
      progress.value = {
        written: data?.written ?? 0,
        total: data?.total ?? 0,
        rate: data?.rate ?? 0,
      };
    }),
    Events.On("wails:updater:verifying", () => (step.value = "verifying")),
    Events.On("wails:updater:installing", () => (step.value = "installing")),
    Events.On("wails:updater:update-ready", () => (step.value = null)),
    Events.On("wails:updater:error", () => (step.value = null)),
  ];
  onScopeDispose(() => offs.forEach((off) => off()));

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
      step.value = null;
      progress.value = null;
    }
  }

  /**
   * Installs what was downloaded and restarts into it. On success the
   * application quits here, so only a failure comes back, in the state.
   */
  async function install() {
    busy.value = true;
    step.value = "installing";
    try {
      await Updater.Service.InstallUpdate();
      await refresh();
    } finally {
      busy.value = false;
      step.value = null;
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

  return {
    state,
    settings,
    busy,
    step,
    progress,
    refresh,
    check,
    download,
    install,
    discard,
    save,
  };
}
