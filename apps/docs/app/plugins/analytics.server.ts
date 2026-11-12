/**
 * The site's analytics: Umami and Rybbit, each only where the deployment has
 * been given its site id (see runtimeConfig in nuxt.config.ts).
 */
export default defineNuxtPlugin(() => {
  const { umamiSiteId, rybbitSiteId } = useRuntimeConfig();
  const script = [];
  if (umamiSiteId) {
    script.push({
      src: "https://umami.anorebel.net/script.js",
      defer: true,
      "data-website-id": umamiSiteId,
    });
  }
  if (rybbitSiteId) {
    script.push({
      src: "https://rybbit.anorebel.net/api/script.js",
      defer: true,
      "data-site-id": rybbitSiteId,
    });
  }
  if (script.length) useHead({ script });
});
