import config from "./playwright.config";
import { defineConfig } from "@playwright/test";
// Run after pnpm build:embed; API responses are deterministic and all provider
// and mail requests are intercepted. No live service is required.
export default defineConfig({
  ...config,
  use: { ...config.use, serviceWorkers: "block" },
  testDir: "./e2e",
  testMatch: "setup.spec.ts",
});
