import js from "@eslint/js";
import accessibility from "eslint-plugin-vuejs-accessibility";
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

  /*
   * Template accessibility, checked where it is written.
   *
   * The Playwright suite runs axe against the built application and catches
   * more than this can — it sees computed contrast and real focus order. This
   * catches a smaller set earlier: a label with no control, an image with no
   * alt text, a click handler on a div. The two overlap and neither replaces
   * the other; a rule here fires in the editor, before the branch is pushed.
   */
  ...accessibility.configs["flat/recommended"],

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

      /*
       * A label wrapping its control is associated with it.
       *
       * The rule defaults to demanding `for` as well, which is belt and
       * braces rather than a fix: nesting is how HTML associates a label, and
       * every assistive technology follows it. Requiring both would mean
       * inventing an id for every checkbox in a list to satisfy a linter.
       */
      /*
       * A label wrapping its control is associated with it.
       *
       * "some" rather than the default "every": nesting is how HTML
       * associates a label, and every assistive technology follows it.
       * Demanding `for` as well would mean inventing an id for each checkbox
       * in a list to satisfy a linter.
       *
       * It still cannot see through a component — a label wrapping our
       * <Checkbox> reads as empty to it, because the <input> is inside the
       * component's own template — so those carry a scoped disable naming
       * the component rather than being rewritten into markup that is worse.
       */
      "vuejs-accessibility/label-has-for": ["error", { required: { some: ["nesting", "id"] } }],

      /*
       * autofocus on the first field of a single-purpose form.
       *
       * The rule's concern is a page that moves focus somewhere unexpected
       * while somebody is reading it. The four pages that use it — sign in,
       * register, first-run setup, recover — exist to be filled in and
       * contain nothing else, so focusing the field the page is about is
       * where a sighted person would click anyway. Anywhere it would be a
       * surprise, it is not used.
       */
      "vuejs-accessibility/no-autofocus": "off",
      // v-html is genuinely dangerous and the warning is right to exist, but
      // the rule cannot see that a value was sanitised. It stays a warning
      // rather than an error so that each use has to be justified in review
      // without blocking the build; the only current use renders markdown that
      // has been through a parser with raw HTML disabled and then DOMPurify.
      "vue/no-v-html": "warn",
    },
  },
);
