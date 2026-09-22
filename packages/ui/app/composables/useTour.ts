import type { Driver, DriveStep } from "driver.js";

import {
  afterReaching,
  afterSeeing,
  nothingSeen,
  readProgress,
  resumeAt,
  shouldOffer,
  type Progress,
  type TourName,
} from "#layers/ui/app/lib/tours";

/*
 * storageKey holds what somebody has been shown, in this browser.
 *
 * Keyed by account rather than by browser, because the record answers "has
 * this person seen this" and a shared machine has more than one person. The
 * library's own persistence is a single global key, which would have shown a
 * new colleague a tour marked complete by whoever used the laptop before them.
 *
 * The account is where this actually lives — a tour given on the web client
 * should not be given again on the desktop one. This is the copy that answers
 * immediately: the server's takes a request to reach, and a tour decided
 * against a moment ago should not reappear while that is in flight.
 */
function storageKey(who: string): string {
  return `tdc.tours.${who}`;
}

/**
 * Introductions to parts of the application, given once.
 *
 * A tour is worth showing to somebody who has not seen it and an obstacle to
 * everybody else, so the whole of this is about not showing one twice. Skipping
 * counts as seeing: somebody who dismissed a tour said what they thought of it
 * more clearly than somebody who sat through it.
 */
export function useTour() {
  const { user } = useAuth();
  const api = useApi();

  /** Who this progress belongs to, or nobody when signed out. */
  const owner = computed(() => user.value?.id ?? "");

  /**
   * What the server holds, once it is known.
   *
   * Shared across every caller of this composable, so the fetch happens once
   * rather than per component that might give a tour.
   */
  const fromServer = useState<Progress | null>("tour-progress", () => null);
  const asked = useState("tour-progress-asked", () => false);

  /**
   * Bumped whenever the record changes, so a screen can react to it.
   *
   * A layout that only checked on mount could not offer a tour somebody asked
   * for from settings: the two share a layout, so navigating between them
   * mounts nothing at all. The button cleared the record and nothing ever
   * looked again — which made "show me around" a button that did nothing
   * visible.
   */
  const changed = useState("tour-progress-changed", () => 0);

  function readLocally(): Progress {
    if (import.meta.server || !owner.value) return nothingSeen();

    try {
      return readProgress(localStorage.getItem(storageKey(owner.value)));
    } catch {
      // A private window, or storage that is refused. One tour offered again
      // is the whole cost.
      return nothingSeen();
    }
  }

  /**
   * What has been seen, from whichever source knows.
   *
   * The server wins where it has answered, because it is the record that
   * follows somebody between clients. Until it has, this browser's own copy
   * answers — so a tour dismissed a moment ago stays dismissed rather than
   * reappearing while a request is in flight.
   */
  function read(): Progress {
    return fromServer.value ?? readLocally();
  }

  function write(progress: Progress) {
    if (import.meta.server || !owner.value) return;

    fromServer.value = progress;

    try {
      localStorage.setItem(storageKey(owner.value), JSON.stringify(progress));
    } catch {
      // The tour still works in this browser; it simply will not be
      // remembered here. The server's copy is the one that matters.
    }
  }

  /**
   * And to the account, so the next client already knows.
   *
   * Failures are swallowed: the tour has been given, this browser has recorded
   * it, and refusing to run a tour because a request failed would be a worse
   * answer than one being offered again on another device.
   */
  function publish(progress: Progress) {
    if (import.meta.server || !owner.value) return;

    void api.put("/api/me/preferences/tours", progress).catch(() => {
      // Offline, or signed out between reading and writing.
    });
  }

  /**
   * Fetches what the account holds, once.
   *
   * Called before deciding whether to offer a tour rather than on mount: most
   * screens never give one, and a request from every screen that might would
   * be a request on every navigation.
   */
  async function load() {
    if (import.meta.server || !owner.value || asked.value) return;
    asked.value = true;

    try {
      const stored = await api.get<unknown>("/api/me/preferences/tours");
      fromServer.value = readProgress(JSON.stringify(stored));
    } catch {
      // Nothing reachable. This browser's own copy answers, which is what it
      // did before any of this existed.
    }
  }

  /**
   * Whether this tour has still to be given.
   *
   * Asks the account first. A tour given on another client is one this person
   * has seen, and offering it again because this browser has no record is the
   * whole thing being avoided.
   */
  async function offered(tour: TourName): Promise<boolean> {
    await load();
    return shouldOffer(read(), tour);
  }

  /**
   * Gives a tour, from wherever it was last left.
   *
   * Loaded on demand rather than bundled into the application: an
   * introduction is seen once and then never again, and everybody would carry
   * the weight of it on every visit thereafter.
   */
  /**
   * The tour currently on screen, if any.
   *
   * Held so it can be taken down when the screen it describes goes away. A
   * tour pointing at a sidebar that has been navigated away from is pointing
   * at nothing, and driver.js answers that by marking an empty div and
   * trapping Tab inside its own buttons.
   */
  let showing: Driver | undefined;

  async function give(tour: TourName, steps: DriveStep[]) {
    if (import.meta.server) return;

    const { driver } = await import("driver.js");
    // The stylesheet travels with it, for the same reason — the library's
    // own, then ours over the top of it.
    await import("driver.js/dist/driver.css");
    await import("#layers/ui/app/assets/css/tour.css");

    /*
     * Reaching the end and leaving early are different answers.
     *
     * Somebody who closed a tour, pressed Escape or clicked away has decided
     * something about it, and asking again would be ignoring that. Somebody
     * whose tab was closed, or who navigated off mid-step, has decided
     * nothing — so that one is left offered, and resumes where it stopped.
     *
     * Without this distinction the resume is unreachable: every exit marks
     * the tour seen, and a tour that is never offered again is never resumed.
     */
    const instance: Driver = driver({
      showProgress: true,
      // The overlay is the point: everything but the thing being explained is
      // dimmed, so there is one place to look.
      overlayOpacity: 0.6,
      // Somebody who has understood should be able to leave at once.
      allowClose: true,
      steps,
      onHighlightStarted: () => {
        const at = instance.getActiveIndex();
        if (at === undefined) return;

        const progress = afterReaching(read(), tour, at);
        write(progress);
        /*
         * Not published per step. Somebody moving through a tour would send a
         * request per step, and what the account needs is where they stopped —
         * which the dismissal below records.
         */
      },
      /*
       * Every deliberate way out, in one place.
       *
       * onDestroyStarted runs before the teardown and covers all of them —
       * the close button, Done on the last step, Escape, a click on the
       * overlay. The narrower hooks do not: onCloseClick misses Escape, and
       * onDestroyed is skipped entirely for a teardown begun inside another
       * hook. Both were tried, and both silently lost dismissals somebody had
       * clearly made, which meant being asked again on the next visit.
       *
       * Providing this disables the default teardown, so the destroy is ours.
       * Omitting it leaves a tour that nothing can close.
       */
      onDestroyStarted: () => {
        const progress = afterSeeing(read(), tour);
        write(progress);
        publish(progress);
        showing = undefined;
        instance.destroy();
      },
    });

    showing = instance;
    instance.drive(resumeAt(read(), tour));
  }

  /**
   * Takes the introduction down without marking it seen.
   *
   * For leaving the screen it describes, which is not somebody dismissing it:
   * they should still be offered it when they come back.
   */
  function withdraw() {
    showing?.destroy();
    showing = undefined;
  }

  /**
   * Forgets a tour, so it can be given again.
   *
   * Offered because somebody who wants to see an introduction again should be
   * able to ask, and because a tour is not a thing to be locked out of.
   */
  function forget(tour: TourName) {
    const progress = read();

    const forgotten = {
      seen: progress.seen.filter((name) => name !== tour),
      reached: { ...progress.reached, [tour]: 0 },
    };

    write(forgotten);
    // Published too, or asking to see a tour again would work on this device
    // and nowhere else.
    publish(forgotten);
    changed.value += 1;
  }

  return { offered, give, forget, withdraw, changed };
}
