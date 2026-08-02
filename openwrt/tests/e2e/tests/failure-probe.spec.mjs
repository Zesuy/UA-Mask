import { test, expect } from '../fixtures.mjs';

test('intentional failure preserves screenshot and trace', async ({ page }) => {
  await page.goto('data:text/html,<h1>failure evidence probe</h1>');
  await expect(page.getByRole('heading', { name: 'deliberately absent heading' })).toBeVisible({ timeout: 500 });
});
