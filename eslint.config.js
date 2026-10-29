import tseslint from "typescript-eslint";

// Why: CLAUDE.md "TypeScript / CSS" rules that a linter can enforce live here; nothing else is enabled.
export default tseslint.config(
  { ignores: ["dist/**", "node_modules/**", "coverage/**", "static/**", "tmp/**", "thinking-exp/**"] },
  {
    files: ["src/**/*.ts"],
    languageOptions: { parser: tseslint.parser },
    plugins: { "@typescript-eslint": tseslint.plugin },
    rules: { "@typescript-eslint/no-explicit-any": "error" },
  },
);
