import { defineConfig, devices } from "@playwright/test";

const port = 18421;

export default defineConfig({
  testDir: "./tests",
  fullyParallel: false,
  workers: 1, // tests share one fresh server and run in order (setup first)
  use: { baseURL: `http://127.0.0.1:${port}`, trace: "retain-on-failure" },
  projects: [
    { name: "desktop", use: { ...devices["Desktop Chrome"], viewport: { width: 1280, height: 800 } } },
  ],
  webServer: {
    command: `PORT=${port} ./start-server.sh`,
    url: `http://127.0.0.1:${port}/api/health`,
    reuseExistingServer: false,
    timeout: 15_000,
  },
});
