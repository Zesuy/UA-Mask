import fs from 'node:fs/promises';
import path from 'node:path';
import process from 'node:process';
import { fileURLToPath } from 'node:url';
import { execFile } from 'node:child_process';
import { promisify } from 'node:util';
import { test as base, expect } from '@playwright/test';

const execFileAsync = promisify(execFile);
const here = path.dirname(fileURLToPath(import.meta.url));
const fixtureScript = path.join(here, 'scripts', 'guest-action.sh');

function safeName(value) {
  return value.replace(/[^A-Za-z0-9_.-]+/g, '-').slice(0, 120);
}

export const test = base.extend({
  telemetry: async ({ page }, use, testInfo) => {
    const pageErrors = [];
    const consoleErrors = [];
    const failedResources = [];
    const requests = [];
    let ubusInFlight = 0;
    let maxUbusInFlight = 0;
    let statusInFlight = 0;
    let maxStatusInFlight = 0;

    function isUbusRequest(request) {
      return new URL(request.url()).pathname.includes('/ubus/');
    }

    function isStatusRequest(request) {
      const payload = request.postData() || '';
      return isUbusRequest(request) && payload.includes('/usr/libexec/uamask-luci') && payload.includes('status');
    }

    page.on('pageerror', error => pageErrors.push(String(error)));
    page.on('console', message => {
      if (message.type() === 'error')
        consoleErrors.push(message.text());
    });
    page.on('request', request => {
      const url = new URL(request.url());
      requests.push({ phase: 'start', method: request.method(), url: `${url.origin}${url.pathname}` });
      if (isUbusRequest(request)) {
        ubusInFlight++;
        maxUbusInFlight = Math.max(maxUbusInFlight, ubusInFlight);
      }
      if (isStatusRequest(request)) {
        statusInFlight++;
        maxStatusInFlight = Math.max(maxStatusInFlight, statusInFlight);
      }
    });
    const finish = request => {
      if (isUbusRequest(request))
        ubusInFlight = Math.max(0, ubusInFlight - 1);
      if (isStatusRequest(request))
        statusInFlight = Math.max(0, statusInFlight - 1);
    };
    page.on('requestfinished', finish);
    page.on('requestfailed', request => {
      finish(request);
      const url = new URL(request.url());
      failedResources.push({ url: `${url.origin}${url.pathname}`, error: request.failure()?.errorText || 'failed' });
    });

    const telemetry = {
      pageErrors,
      consoleErrors,
      failedResources,
      requests,
      get maxUbusInFlight() { return maxUbusInFlight; },
      get maxStatusInFlight() { return maxStatusInFlight; },
      reset() {
        pageErrors.length = 0;
        consoleErrors.length = 0;
        failedResources.length = 0;
        requests.length = 0;
        ubusInFlight = 0;
        maxUbusInFlight = 0;
        statusInFlight = 0;
        maxStatusInFlight = 0;
      }
    };
    await use(telemetry);

    const output = {
      title: testInfo.title,
      status: testInfo.status,
      pageErrors,
      consoleErrors,
      failedResources,
      maxUbusInFlight,
      maxStatusInFlight,
      requests
    };
    const outputDir = path.join(process.env.E2E_RUN_DIR, 'telemetry');
    await fs.mkdir(outputDir, { recursive: true });
    await fs.writeFile(path.join(outputDir, `${safeName(testInfo.titlePath.join('-'))}.json`), `${JSON.stringify(output, null, 2)}\n`);
  },

  login: async ({ page, telemetry }, use) => {
    await use(async () => {
      await page.goto('/cgi-bin/luci/');
      const username = page.locator('input[name="luci_username"]');
      await expect.poll(async () =>
        await page.evaluate(() => Boolean(globalThis.L?.env?.sessionid)) || await username.isVisible()
      ).toBe(true);
      if (!await page.evaluate(() => Boolean(globalThis.L?.env?.sessionid))) {
        await expect(username).toBeVisible();
        await username.fill('root');
        await page.locator('input[name="luci_password"]').fill(process.env.LUCI_PASSWORD || '');
        await page.getByRole('button', { name: 'Log in' }).click();
        await expect.poll(async () => {
          const cookies = await page.context().cookies();
          return cookies.some(cookie => cookie.name === 'sysauth_http' && cookie.value.length > 0);
        }).toBe(true);
        await expect.poll(() => page.evaluate(() => Boolean(globalThis.L?.env?.sessionid))).toBe(true);
        await expect(page.getByRole('heading', { name: 'Authorization Required' })).toBeHidden();
        await page.waitForLoadState('networkidle');
      }
      await expect(page).toHaveURL(/\/cgi-bin\/luci\//);
      telemetry.reset();
    });
  },

  guestAction: async ({}, use) => {
    await use(async action => {
      const allowed = new Set([
        'legacy-running', 'running', 'disabled', 'starting', 'failed',
        'enabled-not-running', 'rejected', 'recovery', 'missing-stats',
        'logs-ready', 'missing-log', 'missing-artifacts', 'clear-log-ready',
        'lifecycle-count', 'effective-config', 'identity'
      ]);
      if (!allowed.has(action))
        throw new Error(`unapproved guest fixture action: ${action}`);
      const result = await execFileAsync(fixtureScript, [action], {
        cwd: here,
        env: process.env,
        timeout: 20_000,
        maxBuffer: 1024 * 1024
      });
      return result.stdout.trim();
    });
  }
});

export async function openRoute(page, login, route) {
  await login();
  await page.goto(`/cgi-bin/luci/admin/services/uamask/${route}`);
  await expect(page).toHaveURL(new RegExp(`/uamask/${route}(?:[/?#]|$)`));
  await expect(page.locator('body')).not.toContainText('404 Not Found');
  await expect(page.locator('body')).not.toContainText('Unhandled exception');
  await expect(page.locator('.cbi-map')).toBeVisible();
}

export async function assertHealthyPage(page, telemetry) {
  await expect(page.locator('body')).not.toContainText(/(^|\s)null(\s|$)/);
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth > document.documentElement.clientWidth);
  expect(overflow).toBe(false);
  expect(telemetry.pageErrors).toEqual([]);
  expect(telemetry.consoleErrors).toEqual([]);
  expect(telemetry.failedResources.filter(entry => /uamask/i.test(entry.url))).toEqual([]);
}

export { expect };
