module.exports = {
  root: true,
  env: { browser: true, es2020: true },
  extends: [
    "eslint:recommended",
    "plugin:@typescript-eslint/recommended",
    "plugin:react-hooks/recommended",
  ],
  parser: "@typescript-eslint/parser",
  parserOptions: { ecmaVersion: 2020, sourceType: "module" },
  plugins: ["@typescript-eslint", "react-refresh"],
  rules: {
    // MVP 阶段：关闭两个噪声较高的告警，保留真正的质量闸门
    // （no-explicit-any / no-unused-vars / react-hooks/rules-of-hooks 仍为 error）。
    "react-refresh/only-export-components": "off",
    "react-hooks/exhaustive-deps": "off",
    "@typescript-eslint/no-explicit-any": "error",
  },
  ignorePatterns: ["dist", "node_modules"],
};
