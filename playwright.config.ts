import { defineConfig } from '@playwright/test';

// The web server runs `kai dashboard` against a throwaway copy of the
// synthetic demo brain (tests/fixtures/demo-brain); build ./kai first.
export default defineConfig({
  testDir: './tests/e2e',
  timeout: 30_000,
  retries: 0,
  // One shared demo brain that some tests write to: run files one at a time.
  workers: 1,
  use: {
    baseURL: 'http://localhost:9234',
    headless: true,
  },
  projects: [
    { name: 'chromium', use: { browserName: 'chromium' } },
  ],
  webServer: {
    command: './tests/e2e/serve-demo.sh',
    url: 'http://localhost:9234/api/info',
    reuseExistingServer: !process.env.CI,
    timeout: 60_000,
  },
});
