import { apiClientKey } from "#layers/ui/app/composables/useApi";
import { createApiClient } from "@tdc/types";

import { createHttpTransport } from "~/lib/transport";

/**
 * Installs the web client's transport.
 *
 * The sending itself lives in lib/transport.ts, where the contract both
 * clients are held to can reach it: a transport that could only run inside
 * Nuxt would be one only one of the two adapters ever had to satisfy.
 */
export default defineNuxtPlugin({
  name: "tdc:api",
  // Before anything that might call the server on mount.
  enforce: "pre",
  setup(nuxtApp) {
    const apiBase = useRuntimeConfig().public.apiBase;

    nuxtApp.vueApp.provide(apiClientKey, createApiClient(createHttpTransport($fetch, apiBase)));
  },
});
