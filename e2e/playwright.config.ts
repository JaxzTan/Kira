import { defineConfig, devices } from '@playwright/test'
import path from 'node:path'
import { E2E_PORT, STATE_FILE } from './server'

export default defineConfig({
  testDir: './tests',
  fullyParallel: false,
  workers: 1, // one shared server + ledger; specs use unique payees and relative assertions
  retries: 0,
  timeout: 60_000,
  expect: { timeout: 7_000 },
  reporter: [['list'], ['html', { open: 'never' }]],
  globalSetup: path.resolve(__dirname, 'global-setup.ts'),
  use: {
    baseURL: `http://localhost:${E2E_PORT}`,
    viewport: { width: 1440, height: 900 },
    storageState: STATE_FILE,
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
    acceptDownloads: true,
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'], viewport: { width: 1440, height: 900 } } }],
})
