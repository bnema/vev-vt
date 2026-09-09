import { defineConfig, devices } from '@playwright/test';

const evidenceFile = process.env.PLAYWRIGHT_JSON_OUTPUT_NAME || 'test-results/browser-evidence.json';

export default defineConfig({
  testDir: './internal/htmlharness',
  timeout: 30_000,
  fullyParallel: true,
  reporter: [['list'], ['json', { outputFile: evidenceFile }]],
  use: {
    headless: true,
    viewport: { width: 1280, height: 720 },
    trace: 'retain-on-failure'
  },
  projects: [
    { name: 'chromium', use: { ...devices['Desktop Chrome'] } },
    { name: 'firefox', use: { ...devices['Desktop Firefox'] } },
    { name: 'webkit', use: { ...devices['Desktop Safari'] } }
  ]
});
