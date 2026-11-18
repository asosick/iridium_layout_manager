import { test, expect } from './fixtures';
import { LayoutPage } from './support/layout';

/**
 * The page works inside a real Iridium panel: full shell, Alpine's CSP build,
 * htmx, the plugin's assets, under the policy SecurityHeadersCSP sends.
 * Every spec also fails on page errors, console errors and CSP violations
 * (fixtures.ts); these tests make the basics explicit.
 */
test.describe('smoke', () => {
  test('S16: the panel shell provides window.Alpine, window.htmx and the CSRF store the script relies on', async ({ page }) => {
    const response = await page.goto('/admin/default');
    expect(response?.status()).toBe(200);
    expect(response?.headers()['content-security-policy']).toMatch(/script-src 'self' 'nonce-/);

    const lm = new LayoutPage(page);
    await lm.ready();
    const globals = await page.evaluate(() => {
      const w = window as any;
      const root = document.querySelector('[data-lm-page]');
      return {
        alpine: typeof w.Alpine?.data,
        htmxAjax: typeof w.htmx?.ajax,
        htmxSwap: typeof w.htmx?.swap,
        csrf: typeof w.Alpine?.store('security')?.csrf,
        component: ['editMode', 'currentLayout', 'selectLayout', 'handleReorder']
          .map(key => `${key}:${typeof w.Alpine.$data(root)[key]}`),
        pluginScript: !!document.querySelector('script[src*="/static/registered/layout-manager/layout-manager.js"]'),
        pluginStyles: !!document.querySelector('link[href*="/static/registered/layout-manager/layout-manager.css"]'),
      };
    });
    expect(globals).toEqual({
      alpine: 'function',
      htmxAjax: 'function',
      htmxSwap: 'function',
      csrf: 'string',
      component: ['editMode:boolean', 'currentLayout:number', 'selectLayout:function', 'handleReorder:function'],
      pluginScript: true,
      pluginStyles: true,
    });
  });

  test('S15: the grid column count set by the inline --lm-cols style applies under the CSP, before and after a swap', async ({ page }) => {
    for (const [slug, columns] of [['default', 3], ['narrow', 1], ['wide', 6]] as const) {
      const lm = new LayoutPage(page, slug);
      await lm.goto();
      expect(await lm.columnCount(), `${slug} on load`).toBe(columns);
      await lm.edit();
      await lm.addBlock('short');
      expect(await lm.columnCount(), `${slug} after an htmx swap`).toBe(columns);
    }
  });

  test('an edit round trip: Edit, add, Done, reload', async ({ page }) => {
    const lm = new LayoutPage(page);
    await lm.goto();
    await expect(lm.page.locator('.lm-empty')).toBeVisible();
    await expect(lm.addButton()).toBeHidden();

    await lm.edit();
    const id = await lm.addBlock('short');
    await expect(lm.block(id).locator('[data-e2e-kind="short"]')).toHaveAttribute('data-e2e-id', id);
    await expect(lm.page.locator('.lm-empty')).toBeHidden();

    await lm.done();
    await expect(page.getByText('Layout saved')).toBeVisible();
    expect(await lm.isEditing()).toBe(false);
    await expect(lm.control(id, 'Remove block')).toBeHidden();

    await lm.reload();
    expect(await lm.order()).toEqual([id]);
    expect(await lm.isEditing()).toBe(false);
    expect(await lm.committedOrder()).toEqual([id]);
  });
});
