import { apiClientKey } from "#layers/ui/app/composables/useApi";
import { createApiClient } from "@tdc/types";
import * as Api from "#bindings/api";

import { createWailsTransport } from "~/lib/transport";

/**
 * Installs the desktop client's transport.
 *
 * The sending itself lives in lib/transport.ts, where the contract both
 * clients are held to can reach it — the same suite runs against this and the
 * web client's, which is the only way to know the two behave alike rather than
 * merely compiling against the same interface.
 */
export default defineNuxtPlugin({
  name: "tdc:api",
  // Before anything that might call the server on mount.
  enforce: "pre",
  setup(nuxtApp) {
    /*
     * The generated binding, which mirrors the Go service. Passed in rather
     * than imported by the transport so the transport can be tested against a
     * stand-in: this one talks to a native host that does not exist in a test
     * process.
     */
    const transport = createWailsTransport(Api.Service);

    nuxtApp.vueApp.provide(apiClientKey, createApiClient(transport));
  },
});
