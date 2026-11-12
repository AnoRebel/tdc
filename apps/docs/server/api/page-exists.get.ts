import { queryCollection } from "@nuxt/content/server";

/**
 * Whether this version of the docs has a page, asked by another version's
 * switcher before it sends somebody here.
 *
 * Each version is its own deployment on its own host, so the answer has to
 * cross origins; it is a yes or no about public pages and safe to give anyone.
 */
export default defineEventHandler(async (event) => {
  setResponseHeader(event, "Access-Control-Allow-Origin", "*");

  const path = String(getQuery(event).path ?? "");
  if (!/^\/[\w\-/]*$/.test(path)) return { exists: false };
  if (path === "/") return { exists: true };

  const page = await queryCollection(event, "docs").path(path).select("path").first();
  return { exists: Boolean(page) };
});
