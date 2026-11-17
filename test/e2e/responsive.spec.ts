import { test, expect, devices } from './fixtures';
import { LayoutPage, layoutOf } from './support/layout';

test.describe('responsive', () => {
  test('S13: on a phone-width screen the grid drops to one column', async ({ page }) => {
    const lm = new LayoutPage(page);
    await lm.plant(layoutOf(['short', 'short', 'short']));
    await page.setViewportSize({ width: 1440, height: 900 });
    await lm.goto();
    expect(await lm.columnCount(), 'desktop').toBe(3);

    // A phone loads the page at its own size.
    await page.setViewportSize({ width: 375, height: 812 });
    await lm.goto();
    const width = (await lm.block((await lm.order())[0]).boundingBox())!.width;
    test.fail(true, `S13: the grid keeps 3 columns at 375px; each block is ${Math.round(width)}px wide`);
    expect(await lm.columnCount(), 'phone').toBe(1);
  });

  test.describe('on a touch phone', () => {
    // The device's browser type is the project's (Chromium) already, and
    // cannot change inside a file.
    const { defaultBrowserType: _, ...phone } = devices['Pixel 7'];
    // A drop swaps the grid in and trips S18's console errors (reorder-drag).
    test.use({ ...phone, allowedPageProblems: [/\[hx-csp\] blocked/] });

    test('a block can be reordered by dragging its handle with a finger', async ({ page }) => {
      const lm = new LayoutPage(page, 'narrow');
      await lm.plant(layoutOf(['short', 'short', 'short']));
      await lm.goto();
      await lm.edit();
      const [a, b, c] = await lm.order();

      const handle = (await lm.block(a).locator('.lm-block-handle').boundingBox())!;
      const target = (await lm.block(c).boundingBox())!;
      const from = { x: handle.x + handle.width / 2, y: handle.y + handle.height / 2 };
      const to = { x: target.x + target.width / 2, y: target.y + target.height / 2 };
      const cdp = await page.context().newCDPSession(page);
      const touch = (type: string, point?: { x: number; y: number }) =>
        cdp.send('Input.dispatchTouchEvent', { type, touchPoints: point ? [{ x: point.x, y: point.y }] : [] });
      await touch('touchStart', from);
      for (let i = 1; i <= 20; i++) {
        await touch('touchMove', { x: from.x + ((to.x - from.x) * i) / 20, y: from.y + ((to.y - from.y) * i) / 20 });
        await page.waitForTimeout(20);
      }
      await page.waitForTimeout(300);
      await touch('touchMove', { x: to.x + 2, y: to.y + 2 });
      await page.waitForTimeout(200);
      await touch('touchEnd');
      await lm.settle();

      expect(await lm.order()).toEqual([b, c, a]);
      expect(await lm.serverOrder()).toEqual([b, c, a]);
    });
  });
});
