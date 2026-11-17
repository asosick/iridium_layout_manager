import { test, expect } from './fixtures';
import { LayoutPage, layoutOf } from './support/layout';

/** Move left / Move right: the keyboard- and pointer-equivalent of a drag. */
test.describe('move buttons', () => {
  test('edge buttons are disabled; each press moves a block one place, on screen and stored', async ({ page }) => {
    const lm = new LayoutPage(page);
    await lm.plant(layoutOf(['short', 'short', 'short', 'short']));
    await lm.goto();
    await lm.edit();
    const [a, b, c, d] = await lm.order();
    await expect(lm.control(a, 'Move block left')).toBeDisabled();
    await expect(lm.control(d, 'Move block right')).toBeDisabled();
    await expect(lm.control(b, 'Move block left')).toBeEnabled();

    await lm.moveRight(a);
    expect(await lm.order()).toEqual([b, a, c, d]);
    expect(await lm.visualOrder()).toEqual([b, a, c, d]);
    await lm.moveLeft(d);
    expect(await lm.visualOrder()).toEqual([b, a, d, c]);
    expect(await lm.serverOrder()).toEqual([b, a, d, c]);
    await expect(lm.control(b, 'Move block left')).toBeDisabled();
    await expect(lm.control(c, 'Move block right')).toBeDisabled();
  });

  test('S1: with a two-column block, Move left moves the block one place on screen', async ({ page }) => {
    const lm = new LayoutPage(page);
    // tall, a, wide (2 columns), b: the hole beside a stays a hole, so the
    // screen shows the stored order.
    await lm.plant(layoutOf(['tall', 'short', ['short', 2], 'short']));
    await lm.goto();
    await lm.edit();
    const [tall, a, wide, b] = await lm.order();
    const before = await lm.visualOrder();
    expect(before, 'screen order before the move').toEqual([tall, a, wide, b]);

    await lm.moveLeft(b);
    expect(await lm.order()).toEqual([tall, a, b, wide]);
    expect(await lm.serverOrder()).toEqual([tall, a, b, wide]);

    expect((await lm.visualOrder()).indexOf(b), 'place of b on screen').toBe(before.indexOf(b) - 1);
    expect(await lm.visualOrder()).toEqual(await lm.order());
  });

  test('a block can be moved with the keyboard alone', async ({ page }) => {
    const lm = new LayoutPage(page);
    await lm.plant(layoutOf(['short', 'short', 'short', 'short']));
    await lm.goto();
    await lm.edit();
    const [a, b, c, d] = await lm.order();

    await lm.control(a, 'Move block right').focus();
    await Promise.all([page.waitForResponse(r => r.url().includes('/lm/move')), page.keyboard.press('Enter')]);
    await lm.waitForIdle();
    expect(await lm.order()).toEqual([b, a, c, d]);
  });

  test('S5: after a keyboard move the focus stays on the control, so a second press moves again', async ({ page }) => {
    const lm = new LayoutPage(page);
    await lm.plant(layoutOf(['short', 'short', 'short', 'short']));
    await lm.goto();
    await lm.edit();
    const [a, b, c, d] = await lm.order();

    await lm.control(a, 'Move block right').focus();
    await Promise.all([page.waitForResponse(r => r.url().includes('/lm/move')), page.keyboard.press('Enter')]);
    await lm.waitForIdle();

    expect(await page.evaluate(() => document.activeElement?.getAttribute('aria-label'))).toBe('Move block right');
    await page.keyboard.press('Enter');
    await lm.settle();
    expect(await lm.order()).toEqual([b, c, a, d]);
  });
});
