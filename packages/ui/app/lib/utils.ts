import { type ClassValue, clsx } from "clsx";
import { twMerge } from "tailwind-merge";

/**
 * Combines class names, resolving Tailwind conflicts so a later class wins.
 *
 * Without the merge, passing a class to override a component's default would
 * leave both in the markup and let specificity decide, which is not what the
 * caller means.
 */
export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}
