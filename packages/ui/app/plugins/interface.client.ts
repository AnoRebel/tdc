/**
 * Applies the interface preferences from the moment the application starts.
 *
 * A plugin rather than a call in each client's root, so neither client can
 * forget it: the head script has already set motion and density before the
 * first paint, and this is what keeps them current afterwards — when somebody
 * changes their choice, or their operating system's.
 */
export default defineNuxtPlugin(() => {
  useInterfacePreferences();
});
