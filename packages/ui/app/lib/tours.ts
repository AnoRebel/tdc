/**
 * Which introductions somebody has already had.
 *
 * A tour is worth showing once. Shown twice it is an obstacle, and shown to
 * somebody who dismissed it deliberately it is worse than that — so the record
 * of having seen one is kept per account rather than per browser, and a tour
 * that was skipped counts as seen just as much as one that was finished.
 */

/** A tour this application knows how to give. */
export type TourName = "chat";

/** What somebody has been shown, and how far they reached. */
export interface Progress {
  /** Tours that will not be offered again, whether finished or skipped. */
  seen: TourName[];
  /**
   * Where an unfinished tour was left, so it can resume rather than restart.
   *
   * Somebody who was interrupted at step four has already read the first
   * three, and beginning again teaches them nothing while costing them the
   * same attention twice.
   */
  reached: Partial<Record<TourName, number>>;
}

/** Nothing seen yet, which is what a new account starts from. */
export function nothingSeen(): Progress {
  return { seen: [], reached: {} };
}

/**
 * Whether a tour should be offered.
 *
 * Only the record of having seen it stops a tour. Being partway through does
 * not: an interrupted tour is one somebody has not finished, and finishing it
 * is the point.
 */
export function shouldOffer(progress: Progress, tour: TourName): boolean {
  return !progress.seen.includes(tour);
}

/**
 * Where a tour should begin.
 *
 * Zero for one nobody has started. Otherwise where they got to, so an
 * interruption costs the reader nothing they had already read.
 */
export function resumeAt(progress: Progress, tour: TourName): number {
  return progress.reached[tour] ?? 0;
}

/**
 * The progress after somebody reached a step.
 *
 * Never moves backwards. A tour that steps back to revisit something should
 * not lose the furthest point already read, or an interruption there would
 * resume earlier than it should.
 */
export function afterReaching(progress: Progress, tour: TourName, step: number): Progress {
  const furthest = Math.max(progress.reached[tour] ?? 0, step);

  return { ...progress, reached: { ...progress.reached, [tour]: furthest } };
}

/**
 * The progress after a tour is finished or skipped.
 *
 * Both are the same answer to "should this be offered again", which is the
 * only question this record exists to settle. Somebody who skipped a tour has
 * said what they think of it more clearly than somebody who sat through it.
 */
export function afterSeeing(progress: Progress, tour: TourName): Progress {
  if (progress.seen.includes(tour)) return progress;

  return { ...progress, seen: [...progress.seen, tour] };
}

/**
 * Reads progress that was stored, discarding anything unrecognisable.
 *
 * A tour is a convenience. Nothing here is worth failing a page load over, so
 * anything that cannot be read is treated as nothing having been seen — the
 * cost of which is one tour offered again.
 */
export function readProgress(stored: string | null): Progress {
  if (!stored) return nothingSeen();

  try {
    const parsed: unknown = JSON.parse(stored);
    if (typeof parsed !== "object" || parsed === null) return nothingSeen();

    const { seen, reached } = parsed as Partial<Progress>;

    return {
      seen: Array.isArray(seen) ? seen.filter((name): name is TourName => name === "chat") : [],
      reached: typeof reached === "object" && reached !== null ? reached : {},
    };
  } catch {
    return nothingSeen();
  }
}
