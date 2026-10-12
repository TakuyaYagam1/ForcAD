import { defineConfig } from "@playwright/test";

const project = process.env.FORCAD_E2E_PROJECT;
if (!project || !/^forcad-deployment-[a-z0-9][a-z0-9_-]*$/.test(project)) {
  throw new Error("FORCAD_E2E_PROJECT must use the isolated forcad-deployment- prefix.");
}

const baseURL = process.env.FORCAD_E2E_BASE_URL || "http://nginx:8080";
let target: URL;
try {
  target = new URL(baseURL);
} catch {
  throw new Error("FORCAD_E2E_BASE_URL must target nginx or localhost.");
}
if (
  !["http:", "https:"].includes(target.protocol) ||
  !["nginx", "localhost", "127.0.0.1", "[::1]"].includes(target.hostname) ||
  target.username !== "" ||
  target.password !== "" ||
  target.pathname !== "/" ||
  target.search !== "" ||
  target.hash !== ""
) {
  throw new Error("FORCAD_E2E_BASE_URL must target nginx or localhost.");
}

export default defineConfig({
  testDir: "./e2e",
  fullyParallel: false,
  workers: 1,
  retries: 0,
  timeout: 180_000,
  expect: { timeout: 15_000 },
  reporter: "list",
  outputDir: process.env.FORCAD_E2E_OUTPUT_DIR || "./test-results",
  use: {
    baseURL,
    browserName: "chromium",
    screenshot: "off",
    trace: "off",
    video: "off",
  },
});
