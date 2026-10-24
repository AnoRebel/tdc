import type { ApiClient, OwnLocation } from "@tdc/types";

/*
 * Keeping a detected location current, quietly.
 *
 * Loaded on its own, after the page, by the application shell when it opens
 * and whenever its window regains focus. It asks at most once every few hours
 * per device, and only ever where permission was already given: it never
 * prompts. Somebody who chose "detect it automatically" agreed to be asked
 * once, in settings — not every time they come back to the window.
 *
 * The position goes to this server, which names the nearest town and keeps the
 * town. Nothing here stores it.
 */

const CHECKED_KEY = "tdc.location.checkedAt";

/** How often a detected location is refreshed while the application is used. */
export const LOCATION_REFRESH_MS = 3 * 60 * 60 * 1000;

/** Whether enough time has passed since this device last looked. */
export function locationRefreshDue(now = Date.now()): boolean {
  try {
    const last = Number(localStorage.getItem(CHECKED_KEY) ?? 0);
    return !Number.isFinite(last) || now - last >= LOCATION_REFRESH_MS;
  } catch {
    return false;
  }
}

function markChecked(now = Date.now()) {
  try {
    localStorage.setItem(CHECKED_KEY, String(now));
  } catch {
    // Without storage it simply asks again next time it is due.
  }
}

/** One reading of the device's position, as a promise. */
export function currentPosition(): Promise<GeolocationPosition> {
  return new Promise((resolve, reject) =>
    navigator.geolocation.getCurrentPosition(resolve, reject, {
      // A town is the answer, so a coarse, recent fix is plenty and spares
      // the battery a GPS lock.
      enableHighAccuracy: false,
      maximumAge: 10 * 60 * 1000,
      timeout: 20_000,
    }),
  );
}

/** Refreshes a detected location if it is due, permitted, and wanted. */
export async function refreshLocation(api: ApiClient): Promise<void> {
  if (!locationRefreshDue()) return;
  markChecked();
  if (!("geolocation" in navigator) || !navigator.permissions) return;

  const permission = await navigator.permissions.query({ name: "geolocation" }).catch(() => null);
  if (permission?.state !== "granted") return;

  const own = await api.get<OwnLocation>("/api/me/location");
  if (own.mode !== "automatic") return;

  const { coords } = await currentPosition();
  await api.post("/api/me/location/detect", {
    latitude: coords.latitude,
    longitude: coords.longitude,
  });
}
