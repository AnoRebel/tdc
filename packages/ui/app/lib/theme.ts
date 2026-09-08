/*
 * The theme decision, made before the first paint.
 *
 * A string rather than a function, because it is inlined into the document
 * head and runs before any of the application has loaded — there is nothing to
 * import it into. Kept here so the config and the test that checks it read the
 * same source, which is the only way a test of a copied string means anything.
 *
 * The palette treats dark as its default: its rule is `.dark, :root:not(.light)`.
 * So what this sets for a light preference is the class that turns that off.
 * Both are set explicitly regardless, so a change to which way the stylesheet
 * defaults cannot silently change what everybody sees.
 *
 * Wrapped in try/catch because localStorage throws in a private window in some
 * browsers, and a theme preference is not worth a blank page.
 */
export const PREPAINT_THEME_SCRIPT =
  "(function(){try{" +
  "var t=localStorage.getItem('tdc-theme');" +
  "var d=t==='dark'||((!t||t==='auto')&&" +
  "window.matchMedia('(prefers-color-scheme: dark)').matches);" +
  "var c=document.documentElement.classList;" +
  "c.toggle('dark',d);c.toggle('light',!d);" +
  "}catch(e){}})();";

/** Where the preference is stored, so the script and the composable agree. */
export const THEME_STORAGE_KEY = "tdc-theme";
