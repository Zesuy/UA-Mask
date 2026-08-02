import AxeBuilder from '@axe-core/playwright';
import { test, expect, openRoute, assertHealthyPage } from '../fixtures.mjs';

async function body(page) {
  return page.locator('body');
}

test.describe.serial('UA-Mask product behavior', () => {
  test('targeted landscape and tablet layouts remain bounded', async ({ page, login, telemetry }) => {
    for (const viewport of [{ width: 812, height: 375 }, { width: 768, height: 1024 }]) {
      for (const route of ['overview', 'settings', 'diagnostics']) {
        await page.setViewportSize(viewport);
        await openRoute(page, login, route);
        await assertHealthyPage(page, telemetry);
      }
    }
  });

  test('overview presents running metrics and factual risks', async ({ page, login, guestAction, telemetry }) => {
    await guestAction('legacy-running');
    await openRoute(page, login, 'overview');
    await expect(await body(page)).toContainText('Running');
    await expect(await body(page)).toContainText('Runtime metrics');
    await expect(await body(page)).toContainText(/original User-Agent|Traffic offload/);
    await assertHealthyPage(page, telemetry);
  });

  for (const state of [
    ['disabled', 'Disabled', 'Disabled'],
    ['starting', 'Starting', 'Starting'],
    ['failed', 'Failed', 'Failed'],
    ['enabled-not-running', 'Enabled but not running', 'Enabled but not running'],
    ['rejected', 'Running with configuration error', 'latest candidate configuration was rejected']
  ]) {
    test(`overview and diagnostics present ${state[0]} state`, async ({ page, login, guestAction, telemetry }) => {
      await guestAction(state[0]);
      await openRoute(page, login, 'overview');
      await expect(await body(page)).toContainText(state[1]);
      await openRoute(page, login, 'diagnostics');
      await expect(await body(page)).toContainText(state[2]);
      await assertHealthyPage(page, telemetry);
    });
  }

  test('recovery clears the rejected error', async ({ page, login, guestAction }) => {
    await guestAction('recovery');
    await openRoute(page, login, 'overview');
    await expect(await body(page)).toContainText('Running');
    await expect(await body(page)).not.toContainText('Running with configuration error');
  });

  test('missing stats is explicit and bounded', async ({ page, login, guestAction }) => {
    await guestAction('missing-stats');
    await openRoute(page, login, 'overview');
    await expect(await body(page)).toContainText('Statistics are not available yet');
  });

  test('overview polls at five-second cadence without overlap or runaway after detach', async ({ page, login, guestAction, telemetry }) => {
    await guestAction('running');
    await openRoute(page, login, 'overview');
    const starts = [];
    page.on('request', request => {
      const payload = request.postData() || '';
      if (request.url().includes('/ubus/') && payload.includes('/usr/libexec/uamask-luci') && payload.includes('status'))
        starts.push(Date.now());
    });
    await expect.poll(() => starts.length, { timeout: 23_000, intervals: [500] }).toBeGreaterThanOrEqual(3);
    const interval = starts.at(-1) - starts.at(-2);
    expect(interval).toBeGreaterThanOrEqual(4500);
    // The implementation schedules a five-second poll. Allow bounded host/QEMU
    // scheduling jitter without turning a slow guest into a product failure.
    expect(interval).toBeLessThanOrEqual(7000);
    expect(telemetry.maxStatusInFlight).toBeLessThanOrEqual(1);
    await page.goto('/cgi-bin/luci/admin/status/overview');
    const detachedCount = starts.length;
    await page.waitForTimeout(5500);
    expect(starts.length).toBe(detachedCount);
  });

  test('legacy scalar lists render separately and unchanged values canonicalize', async ({ page, login, guestAction }) => {
    await guestAction('legacy-running');
    const before = JSON.parse(await guestAction('effective-config'));
    await openRoute(page, login, 'settings');
    for (const item of ['Android', 'Legacy Phone', 'ExactUA/1.0', 'ExactUA/2.0', 'Offload Legacy', '22', '80', '443', '127.0.0.0/8'])
      await expect(await body(page)).toContainText(item);
    const baseline = Number(await guestAction('lifecycle-count'));
    await page.locator('.cbi-button-save').click();
    await expect.poll(async () => Number(await guestAction('lifecycle-count'))).toBe(baseline);
    await page.locator('.cbi-button-apply').click();
    await expect(page.locator('.alert-message')).toContainText(/lifecycle action completed|Settings saved/);
    await expect.poll(async () => Number(await guestAction('lifecycle-count'))).toBe(baseline + 1);
    const after = JSON.parse(await guestAction('effective-config'));
    expect(after).toEqual(before);
  });

  test('blank list add controls save while malformed port and IP are actionable', async ({ page, login, guestAction }) => {
    await guestAction('running');
    await openRoute(page, login, 'settings');
    const blankKeyword = page.locator('#widget\\.cbid\\.UAmask\\.main\\.keywords');
    await blankKeyword.fill('');
    await page.locator('.cbi-button-save').click();
    await expect(blankKeyword).not.toHaveClass(/cbi-input-invalid/);

    await page.locator('a').filter({ hasText: 'Traffic scope' }).click();
    const port = page.locator('#widget\\.cbid\\.UAmask\\.main\\.bypass_ports');
    await port.pressSequentially('70000');
    await expect(port).toHaveAttribute('data-tooltip', /valid port value|between 1 and 65535/i);
    await port.fill('');
    const ip = page.locator('#widget\\.cbid\\.UAmask\\.main\\.bypass_ips');
    await ip.pressSequentially('999.1.1.1/33');
    await expect(ip).toHaveAttribute('data-tooltip', /valid IPv4|IPv4 octet|\/0 through \/32/i);
  });

  test('invalid regex apply is actionable and preserves healthy PID and JSON', async ({ page, login, guestAction }) => {
    await guestAction('running');
    const identityBefore = JSON.parse(await guestAction('identity'));
    await openRoute(page, login, 'settings');
    await page.locator('a').filter({ hasText: 'User-Agent matching' }).click();
    await page.locator('#widget\\.cbid\\.UAmask\\.main\\.match_mode').selectOption('regex');
    const regex = page.locator('#widget\\.cbid\\.UAmask\\.main\\.ua_regex');
    await regex.fill('(');
    await page.locator('.cbi-button-apply').click();
    await expect(page.locator('.alert-message')).toContainText(/reload failed|Open Diagnostics/);
    const identityAfter = JSON.parse(await guestAction('identity'));
    expect(identityAfter.pid).toBe(identityBefore.pid);
    expect(identityAfter.config_sha256).toBe(identityBefore.config_sha256);
  });

  test('keyboard operation, focus visibility, labels and semantics are present', async ({ page, login, guestAction }) => {
    await guestAction('running');
    await openRoute(page, login, 'settings');
    const enabled = page.locator('input[type="checkbox"]').first();
    await enabled.focus();
    const before = await enabled.isChecked();
    await page.keyboard.press('Space');
    expect(await enabled.isChecked()).toBe(!before);
    await page.keyboard.press('Space');
    expect(await enabled.isChecked()).toBe(before);
    await page.keyboard.press('Tab');
    const focusVisible = await page.evaluate(() => document.activeElement !== document.body && document.activeElement.matches(':focus-visible'));
    expect(focusVisible).toBe(true);
    await expect(page.locator('input:visible, select:visible, textarea:visible').first()).toHaveAttribute('id', /.+/);
    await openRoute(page, login, 'diagnostics');
    expect(await page.locator('[role="status"], [role="alert"]').count()).toBeGreaterThan(0);
  });

  test('diagnostics handles bounded logs, refresh, clear cancel and confirm', async ({ page, login, guestAction }) => {
    await guestAction('logs-ready');
    await openRoute(page, login, 'diagnostics');
    const logArea = page.getByLabel('Recent UA-Mask logs');
    await expect(logArea).toContainText('fixture-line-250');
    await expect(logArea).not.toContainText('fixture-line-001');
    const refreshLogs = page.getByRole('button', { name: 'Refresh logs' });
    await expect(refreshLogs).toBeEnabled();
    await refreshLogs.click();
    await expect(page.getByRole('status').filter({ hasText: /refreshed/i })).toContainText(/refreshed/i);
    const clearLogs = page.getByRole('button', { name: 'Clear logs' });
    await expect(clearLogs).toBeEnabled();
    await clearLogs.click();
    await expect(page.getByRole('dialog')).toBeVisible();
    await page.keyboard.press('Escape');
    await expect(page.getByRole('dialog')).toBeHidden();
    await expect(clearLogs).toBeEnabled();
    await clearLogs.click();
    const confirm = page.getByRole('dialog').getByRole('button', { name: 'Clear logs' });
    await confirm.focus();
    await page.keyboard.press('Enter');
    await expect(page.getByRole('status').filter({ hasText: 'Logs cleared' })).toContainText('Logs cleared');
    await expect(logArea).toContainText('No log lines are available');
  });

  test('diagnostics presents missing log and missing artifacts with retry paths', async ({ page, login, guestAction }) => {
    await guestAction('missing-log');
    await openRoute(page, login, 'diagnostics');
    await expect(await body(page)).toContainText(/configured log is unavailable|log is unavailable/i);
    await expect(page.getByRole('button', { name: 'Retry' }).first()).toBeVisible();
    await guestAction('missing-artifacts');
    await page.reload();
    await expect(await body(page)).toContainText(/no error artifact|missing or unreadable/i);
    await expect(await body(page)).toContainText(/effective configuration is unavailable/i);
  });

  test('diagnostics rejected, failed and recovery errors are current and accessible', async ({ page, login, guestAction }) => {
    for (const action of ['rejected', 'failed']) {
      await guestAction(action);
      await openRoute(page, login, 'diagnostics');
      await expect(page.getByRole('alert').filter({ hasText: /validation|failed|rejected/i }).first()).toBeVisible();
      await expect(await body(page)).toContainText(/validation|failed|rejected/i);
    }
    await guestAction('recovery');
    await page.reload();
    await expect(await body(page)).toContainText('No validation or service-start error is recorded');
  });

  test('all three application roots pass focused axe checks in dark reduced-motion mode', async ({ page, login }) => {
    await page.emulateMedia({ colorScheme: 'dark', reducedMotion: 'reduce' });
    for (const route of ['overview', 'settings', 'diagnostics']) {
      await openRoute(page, login, route);
      const results = await new AxeBuilder({ page })
        .include('.cbi-map')
        .exclude('.cbi-map input[type="checkbox"][data-widget-id^="widget.cbid.UAmask."]')
        .withTags(['wcag2a', 'wcag2aa'])
        .analyze();
      expect(results.violations).toEqual([]);
    }
  });
});
