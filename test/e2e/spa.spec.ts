import { test, expect } from './fixtures';
import { LayoutPage, layoutOf } from './support/layout';

/**
 * Iridium navigates between panel pages without a reload (htmx swaps the
 * content). The page's Alpine controller must clean up after itself when its
 * page leaves, or its document-level hotkey listener outlives it.
 */
async function navigate(page: import('@playwright/test').Page, label: string, path: string) {
  await page.getByRole('link', { name: label, exact: true }).first().click();
  await page.waitForURL(`**${path}`);
}

test.describe('single-page navigation', () => {
  test('S11: after navigating away, the layout page\'s hotkeys no longer act', async ({ page }) => {
    const lm = new LayoutPage(page);
    await lm.plant(layoutOf(['short'], ['tall'], []));
    await lm.goto();
    await navigate(page, 'Elsewhere', '/admin/elsewhere');
    await expect(page.locator('[data-e2e-elsewhere]')).toBeVisible();
    expect(await page.evaluate(() => document.querySelectorAll('[data-lm-page]').length)).toBe(0);

    await page.evaluate(() => {
      window.addEventListener('keydown', e => { (window as any).__prevented = e.defaultPrevented; });
    });
    await page.keyboard.press('Control+2');
    await lm.settle();
    expect(lm.requests.filter(r => r.includes('/lm/select')), 'requests from a page that is gone').toEqual([]);
    expect(await page.evaluate(() => (window as any).__prevented), 'key swallowed by a page that is gone').toBe(false);
  });

  test('S11: after navigating away and back three times, one Ctrl/Cmd+2 sends one request', async ({ page }) => {
    const lm = new LayoutPage(page);
    await lm.plant(layoutOf(['short'], ['tall'], []));
    await lm.goto();
    for (let i = 0; i < 3; i++) {
      await navigate(page, 'Elsewhere', '/admin/elsewhere');
      await navigate(page, 'Default', '/admin/default');
      await lm.ready();
    }

    await Promise.all([page.waitForResponse(r => r.url().includes('/lm/select')), page.keyboard.press('Control+2')]);
    await lm.settle();
    expect(lm.requests.filter(r => r.includes('/lm/select'))).toHaveLength(1);
    expect(await lm.order()).toEqual(['tall-2']);
  });
});
