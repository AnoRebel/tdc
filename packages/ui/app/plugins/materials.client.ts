import { loadMaterials, needsMaterials } from "#layers/ui/app/lib/materials";

/**
 * A chosen material, there from the first frame.
 *
 * The head script has already put its name on the page; this waits for its
 * stylesheet before the application mounts, so somebody who chose Tactile
 * never sees a frame of the default first. Nothing is fetched for anybody else.
 */
export default defineNuxtPlugin(async () => {
  if (needsMaterials(document.documentElement)) await loadMaterials();
});
