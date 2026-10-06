/**
 * Closes a ring's notification when the ring ends.
 *
 * The service worker shows somebody calling while this browser is in the
 * background; the call window says when that ring is over, answered on another
 * device or given up, and the notification goes with it.
 */
export default defineNuxtPlugin(() => {
  window.addEventListener("tdc:ring-ended", (event) => {
    const call = (event as CustomEvent<string>).detail;
    void navigator.serviceWorker
      ?.getRegistration()
      .then((registration) => registration?.getNotifications({ tag: `call:${call}` }))
      .then((shown) => shown?.forEach((each) => each.close()))
      .catch(() => {});
  });
});
