import { fileURLToPath } from "node:url";
import { defineConfig } from "vitest/config";

const here = fileURLToPath(new URL(".", import.meta.url));

/**
 * The desktop client's own unit tests.
 *
 * Separate from the web application's because the aliases differ: the generated
 * bindings and the menu event names exist only here, and a shared configuration
 * would either fail to resolve them or resolve them for an application that has
 * neither.
 */
export default defineConfig({
  test: {
    include: ["app/**/*.test.ts"],
    exclude: ["**/node_modules/**", "**/dist/**", "**/.nuxt/**"],
    fsModuleCache: true,
  },

  resolve: {
    alias: {
      // The same names nuxt.config.ts gives them, since a test importing what
      // the application imports is the only version worth running.
      "~": `${here}app`,
      "#bindings": `${here}bindings/github.com/AnoRebel/tdc/apps/desktop/internal`,
    },
  },
});
