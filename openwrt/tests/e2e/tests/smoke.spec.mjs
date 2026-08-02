import AxeBuilder from '@axe-core/playwright';
import { test, expect, openRoute, assertHealthyPage } from '../fixtures.mjs';

const routes = ['overview', 'settings', 'diagnostics'];
const viewports = [
  { name: 'desktop', width: 1440, height: 900 },
  { name: 'phone', width: 375, height: 812 }
];

test.describe.serial('native LuCI smoke', () => {
  test.beforeAll(async ({ guestAction }) => {
    await guestAction('legacy-running');
  });

  for (const viewport of viewports) {
    for (const route of routes) {
      test(`${route} ${viewport.name} renders without compatibility errors`, async ({ page, login, telemetry }) => {
        await page.setViewportSize(viewport);
        await page.emulateMedia({ colorScheme: viewport.name === 'phone' ? 'dark' : 'light', reducedMotion: 'reduce' });
        await openRoute(page, login, route);
        await expect(page.locator('body')).not.toContainText(/Lua|luci-compat/i);
        await assertHealthyPage(page, telemetry);
      });
    }
  }

  test('UA-Mask application root passes high-impact axe checks', async ({ page, login }) => {
    await openRoute(page, login, 'settings');
    const results = await new AxeBuilder({ page })
      .include('.cbi-map')
      // LuCI 23.05 Flag widgets generate an empty internal <label>; exclude
      // only those framework-owned checkbox nodes, not the surrounding field.
      .exclude('.cbi-map input[type="checkbox"][data-widget-id^="widget.cbid.UAmask."]')
      .withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'])
      .analyze();
    expect(results.violations).toEqual([]);
  });
});
