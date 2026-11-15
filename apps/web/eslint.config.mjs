import { defineConfig, globalIgnores } from "eslint/config";
import nextVitals from "eslint-config-next/core-web-vitals";
import nextTs from "eslint-config-next/typescript";

const eslintConfig = defineConfig([
  ...nextVitals,
  ...nextTs,
  // Override default ignores of eslint-config-next.
  globalIgnores([
    // Default ignores of eslint-config-next:
    ".next/**",
    "out/**",
    "build/**",
    "next-env.d.ts",
  ]),
  {
    rules: {
      // The realtime protocol and the API boundary legitimately carry dynamic
      // payloads; the types are documented in lib/types.ts.
      "@typescript-eslint/no-explicit-any": "off",
      // React 19's compiler-era rule flags the classic "initialize from
      // localStorage / reset list on query change" patterns used intentionally
      // across forms and search UIs. Downgraded to a warning (not a blocker)
      // until the app migrates to server components for these flows.
      "react-hooks/set-state-in-effect": "warn",
    },
  },
]);

export default eslintConfig;
