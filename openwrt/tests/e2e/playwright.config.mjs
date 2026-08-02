import path from 'node:path';
import process from 'node:process';
import { defineConfig } from '@playwright/test';

const runDir = process.env.E2E_RUN_DIR || path.resolve('../../../dev/luci-e2e/unmanaged');
const executablePath = process.env.CHROMIUM_PATH || undefined;

export default defineConfig({
  testDir: './tests',
  timeout: 45_000,
  expect: { timeout: 10_000 },
  fullyParallel: false,
  workers: 1,
  retries: 0,
  forbidOnly: true,
  outputDir: path.join(runDir, 'test-results'),
  reporter: [
    ['list'],
    ['html', { outputFolder: path.join(runDir, 'playwright-report'), open: 'never' }]
  ],
  use: {
    baseURL: process.env.LUCI_BASE_URL,
    browserName: 'chromium',
    launchOptions: executablePath ? { executablePath } : {},
    headless: true,
    ignoreHTTPSErrors: false,
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
    video: 'off',
    actionTimeout: 10_000,
    navigationTimeout: 15_000
  }
});
