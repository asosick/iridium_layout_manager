import { defineConfig, devices } from '@playwright/test';

/**
 * Browser end-to-end specs for the layout manager.
 *
 * Global setup builds the test server (test/e2e/server) once; every worker
 * starts its own copy on a free port (test/e2e/fixtures.ts). Layout state
 * lives in the browser's cookies, so each test's fresh browser context starts
 * with an empty layout.
 *
 * On a shared machine run the whole suite under the build lock:
 *   buildlock npx playwright test
 * To test a server that is already running instead:
 *   E2E_SERVER_URL=http://127.0.0.1:18788 npx playwright test --workers=1
 */
export default defineConfig<{}, { e2eTLS: boolean }>({
  testDir: './test/e2e',
  testMatch: '**/*.spec.ts',
  timeout: 30_000,
  expect: { timeout: 5_000 },
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  reporter: [
    ['list'],
    ['html', { outputFolder: 'test/e2e/report', open: 'never' }],
  ],
  globalSetup: './test/e2e/global-setup.ts',
  use: {
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
  },
  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
    // Dragging goes through each engine's native drag and drop (and the
    // gallery's cards through its pointer events), and block spans per
    // breakpoint through each engine's CSS round(), so those specs also run
    // in Firefox and WebKit, over HTTPS: WebKit refuses the plugin's Secure
    // cookies on plain HTTP, even on loopback (S17).
    {
      name: 'firefox',
      testMatch: ['**/reorder-drag.spec.ts', '**/resize.spec.ts', '**/gallery.spec.ts', '**/breakpoints.spec.ts'],
      use: { ...devices['Desktop Firefox'], e2eTLS: true, ignoreHTTPSErrors: true },
    },
    {
      name: 'webkit',
      testMatch: ['**/reorder-drag.spec.ts', '**/resize.spec.ts', '**/gallery.spec.ts', '**/breakpoints.spec.ts'],
      use: { ...devices['Desktop Safari'], e2eTLS: true, ignoreHTTPSErrors: true },
    },
  ],
});
