import { test, expect } from './fixtures';
import { LayoutPage, layoutOf } from './support/layout';

/**
 * Edits made before the previous one answered. Two `slow` blocks make every
 * edit take the server 600ms or more, as a page of real widgets would, so
 * these are ordinary clicks at an ordinary pace, not contrived timing.
 */

test.describe('races', () => {
  test('S3: two quick clicks on + grow the block by two', async ({ page }) => {
    const lm = new LayoutPage(page);
    await lm.plant(layoutOf(['short', 'slow', 'slow']));
    await lm.goto();
    await lm.edit();
    const [a] = await lm.order();

    await lm.control(a, 'Increase size').click();
    await page.waitForTimeout(100);
    await lm.control(a, 'Increase size').click();
    await page.waitForTimeout(1500);
    await lm.waitForIdle();

    expect(await lm.cols(a), 'on screen').toBe(3);
    expect((await lm.storedState()).draft!.layouts![0].blocks![0].cols, 'stored').toBe(3);
  });

  test('S3: a block removed while a drag\'s reorder is in flight stays removed, and the drag stays applied', async ({ page }) => {
    const lm = new LayoutPage(page, 'narrow');
    await lm.plant(layoutOf(['short', 'short', 'short', 'slow', 'slow']));
    await lm.goto();
    await lm.edit();
    const [a, b, c, s1, s2] = await lm.order();

    const reorder = page.waitForResponse(r => r.url().includes('/lm/reorder'));
    await lm.drag(c, a);
    await lm.control(b, 'Remove block').click();
    await reorder;
    await page.waitForTimeout(1500);
    await lm.waitForIdle();

    expect(await lm.order(), 'on screen').toEqual([c, a, s1, s2]);
    expect(await lm.serverOrder(), 'stored').toEqual([c, a, s1, s2]);
  });

  test('Done pressed while a reorder is in flight waits for it, then commits it', async ({ page }) => {
    const lm = new LayoutPage(page, 'narrow');
    await lm.plant(layoutOf(['short', 'short', 'short']));
    await lm.goto();
    await lm.edit();
    const [a, b, c] = await lm.order();
    let answer!: () => void;
    const held = new Promise<void>(resolve => { answer = resolve; });
    await page.route('**/lm/reorder**', async route => { await held; await route.continue(); });

    await lm.drag(a, c);
    await lm.doneButton().click();
    await page.waitForTimeout(400);
    expect(lm.requests.filter(r => r.includes('/lm/save')), 'Done waits for the reorder').toEqual([]);

    answer();
    await page.waitForResponse(r => r.url().includes('/lm/save'));
    await lm.waitForIdle();
    expect(await lm.committedOrder()).toEqual([b, c, a]);
    expect(await lm.isEditing()).toBe(false);
  });

  test('S3: Done pressed while a resize is in flight commits the resize', async ({ page }) => {
    const lm = new LayoutPage(page);
    await lm.plant(layoutOf(['short', 'slow', 'slow']));
    await lm.goto();
    await lm.edit();
    const [a] = await lm.order();

    await lm.control(a, 'Increase size').click();
    await page.waitForTimeout(100);
    await Promise.all([page.waitForResponse(r => r.url().includes('/lm/save')), lm.doneButton().click()]);
    await page.waitForTimeout(1500);
    await lm.waitForIdle();
    expect(await lm.cols(a), 'the resize landed').toBe(2);

    const { draft, committed } = await lm.storedState();
    expect(committed!.layouts![0].blocks![0].cols, 'committed').toBe(2);
    expect(draft!.layouts![0].blocks![0].cols, 'draft').toBe(2);
  });
});
