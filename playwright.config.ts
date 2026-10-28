import { defineConfig } from '@playwright/test';

// The web server runs `kai dashboard` against a throwaway copy of the
// synthetic demo brain (tests/fixtures/demo-brain); build ./kai first.
export default defineConfig({
  testDir: './tests/e2e',
  timeout: 60_000,
  // Headless Chromium renders WebGL in software (a few fps), which slows
  // timers and paints while the scene runs, more so on a shared machine.
  expect: { timeout: 20_000 },
  retries: 0,
  // One shared demo brain that some tests write to: run files one at a time.
  workers: 1,
  use: {
    baseURL: 'http://localhost:9234',
    headless: true,
  },
  projects: [
    { name: 'chromium', use: { browserName: 'chromium' }, testIgnore: /small\.spec\.ts/ },
    // Small screens (< 900 px layout): a phone and a tablet in portrait.
    { name: 'phone', use: { browserName: 'chromium', viewport: { width: 390, height: 844 }, hasTouch: true }, testMatch: /small\.spec\.ts/ },
    { name: 'tablet', use: { browserName: 'chromium', viewport: { width: 768, height: 1024 }, hasTouch: true }, testMatch: /small\.spec\.ts/ },
  ],
  webServer: {
    command: './tests/e2e/serve-demo.sh',
    url: 'http://localhost:9234/api/info',
    reuseExistingServer: !process.env.CI,
    timeout: 60_000,
  },
});
