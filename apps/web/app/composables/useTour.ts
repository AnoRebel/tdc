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
} from "~/lib/tours";

/*
 * storageKey holds what somebody has been shown.
 *
 * Keyed by account rather than by browser, because the record answers "has
 * this person seen this" and a shared machine has more than one person. The
 * library's own persistence is a single global key, which would have shown a
 * new colleague a tour marked complete by whoever used the laptop before them.
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

  /** Who this progress belongs to, or nobody when signed out. */
  const owner = computed(() => user.value?.id ?? "");

  function read(): Progress {
    if (import.meta.server || !owner.value) return nothingSeen();

    try {
      return readProgress(localStorage.getItem(storageKey(owner.value)));
    } catch {
      // A private window, or storage that is refused. One tour offered again
      // is the whole cost.
      return nothingSeen();
    }
  }

  function write(progress: Progress) {
    if (import.meta.server || !owner.value) return;

    try {
      localStorage.setItem(storageKey(owner.value), JSON.stringify(progress));
    } catch {
      // The tour still works; it simply will not be remembered.
    }
  }

  /** Whether this tour has still to be given. */
  function offered(tour: TourName): boolean {
    return shouldOffer(read(), tour);
  }

  /**
   * Gives a tour, from wherever it was last left.
   *
   * Loaded on demand rather than bundled into the application: an
   * introduction is seen once and then never again, and everybody would carry
   * the weight of it on every visit thereafter.
   */
  async function give(tour: TourName, steps: DriveStep[]) {
    if (import.meta.server) return;

    const { driver } = await import("driver.js");
    // The stylesheet travels with it, for the same reason — the library's
    // own, then ours over the top of it.
    await import("driver.js/dist/driver.css");
    await import("~/assets/css/tour.css");

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
    const done = () => {
      write(afterSeeing(read(), tour));
      instance.destroy();
    };

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
        if (at !== undefined) write(afterReaching(read(), tour, at));
      },
      /*
       * Deliberate ways out: the close button and, on the last step, Done.
       *
       * Both record and then tear down, in that order. Recording in
       * onDestroyed instead would never run: driver.js does not call that
       * hook for a teardown started from inside another hook, which is a
       * quiet way to lose every completion — the tour ends, nothing is
       * written, and it is offered again forever.
       *
       * Providing these hooks also disables the default behaviour, so the
       * destroy is ours to do. Omitting it leaves a tour nothing can close.
       */
      onCloseClick: done,
      onDoneClick: done,
    });

    instance.drive(resumeAt(read(), tour));
  }

  /**
   * Forgets a tour, so it can be given again.
   *
   * Offered because somebody who wants to see an introduction again should be
   * able to ask, and because a tour is not a thing to be locked out of.
   */
  function forget(tour: TourName) {
    const progress = read();

    write({
      seen: progress.seen.filter((name) => name !== tour),
      reached: { ...progress.reached, [tour]: 0 },
    });
  }

  return { offered, give, forget };
}
