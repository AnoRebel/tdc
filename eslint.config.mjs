import js from "@eslint/js";
import vue from "eslint-plugin-vue";
import globals from "globals";
import typescript from "typescript-eslint";
import vueParser from "vue-eslint-parser";

/**
 * The shared rule set.
 *
 * Formatting is prettier's job, so nothing here reports on whitespace, quotes
 * or semicolons — a rule that disagrees with the formatter only produces
 * conflicts. What is left is the part a formatter cannot see: unused code,
 * unsafe patterns, and Vue's own correctness rules.
 */
export default typescript.config(
  {
    ignores: [
      "**/.nuxt/**",
      "**/.output/**",
      "**/dist/**",
      "**/node_modules/**",
      "**/bin/**",
      "server/**",
      "apps/desktop/build/**",
      // Written by `wails3 generate bindings` from the Go services. Style
      // rules on generated code are a diff to fix on every regeneration.
      "apps/desktop/frontend/bindings/**",
      // Written by `go generate ./internal/shell`.
      "apps/desktop/frontend/app/lib/menu-events.ts",
    ],
  },

  js.configs.recommended,
  ...typescript.configs.recommended,
  ...vue.configs["flat/recommended"],

  {
    languageOptions: {
      ecmaVersion: "latest",
      sourceType: "module",
      globals: { ...globals.browser, ...globals.node },
    },
    rules: {
      // An unused argument prefixed with _ is a deliberate signature match,
      // not an oversight.
      "@typescript-eslint/no-unused-vars": [
        "error",
        { argsIgnorePattern: "^_", varsIgnorePattern: "^_" },
      ],
      // `any` defeats the point of the strict compiler settings, but a warning
      // rather than an error so it does not block a work in progress.
      "@typescript-eslint/no-explicit-any": "warn",
      "no-console": ["warn", { allow: ["warn", "error"] }],
      // TypeScript resolves identifiers itself, and reports the ones that do
      // not exist far more accurately than this rule can. Leaving it on would
      // mean an error on every Nuxt auto-import — ref, computed, useRoute —
      // which are real bindings the compiler already knows about.
      "no-undef": "off",
    },
  },

  {
    /*
     * Scripts run from a terminal, where printing is the point.
     *
     * The rule is right everywhere else: a stray console.log in the
     * application ships to everybody's browser console. A build check that
     * cannot report what it measured is no use to anybody.
     */
    files: ["**/scripts/**/*.ts", "scripts/**/*.ts"],
    rules: {
      "no-console": "off",
    },
  },

  {
    files: ["**/*.vue"],
    languageOptions: {
      parser: vueParser,
      parserOptions: { parser: typescript.parser, sourceType: "module" },
    },
    rules: {
      // Nuxt auto-imports components and resolves single-word names such as
      // `Avatar` from the components directory, so this rule fires on correct
      // code.
      "vue/multi-word-component-names": "off",
      // Formatting again: prettier owns how attributes wrap.
      "vue/max-attributes-per-line": "off",
      "vue/singleline-html-element-content-newline": "off",
      "vue/html-self-closing": "off",
      "vue/html-indent": "off",
      "vue/html-closing-bracket-newline": "off",
      "vue/attributes-order": "off",
      // An optional pass-through `class` has no sensible default — undefined
      // is exactly what the class merger expects when nothing is given.
      "vue/require-default-prop": "off",
      // v-html is genuinely dangerous and the warning is right to exist, but
      // the rule cannot see that a value was sanitised. It stays a warning
      // rather than an error so that each use has to be justified in review
      // without blocking the build; the only current use renders markdown that
      // has been through a parser with raw HTML disabled and then DOMPurify.
      "vue/no-v-html": "warn",
    },
  },
);
