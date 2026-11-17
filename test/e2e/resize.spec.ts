import { test, expect } from './fixtures';
import { LayoutPage, layoutOf } from './support/layout';

/** Column spans: the size buttons, the full-width toggle and the drag handle. */
test.describe('resize', () => {
  test('+, - and the full-width toggle change the span within 1..GridColumns, on screen and stored', async ({ page }) => {
    const lm = new LayoutPage(page);
    await lm.plant(layoutOf(['short', 'short']));
    await lm.goto();
    await lm.edit();
    const [a] = await lm.order();
    const width = async () => (await lm.block(a).boundingBox())!.width;
    const one = await width();

    await lm.grow(a);
    expect(await lm.cols(a)).toBe(2);
    expect(await width()).toBeGreaterThan(one * 1.9);
    await lm.grow(a);
    await lm.grow(a);
    expect(await lm.cols(a), 'capped at the grid').toBe(3);
    await lm.shrink(a);
    expect(await lm.cols(a)).toBe(2);
    await lm.toggleFull(a);
    expect(await lm.cols(a)).toBe(3);
    await lm.toggleFull(a);
    expect(await lm.cols(a)).toBe(1);
    await lm.shrink(a);
    expect(await lm.cols(a), 'never below one column').toBe(1);
    await lm.grow(a);

    await lm.done();
    await lm.reload();
    expect(await lm.cols(a)).toBe(2);
  });

  test('the drag handle resizes, and the guide highlights the columns the block will cover', async ({ page }) => {
    const lm = new LayoutPage(page);
    await lm.plant(layoutOf(['short', 'short']));
    await lm.goto();
    await lm.edit();
    const [a] = await lm.order();

    // Hold the drag over two columns and look at the guide.
    const handle = lm.block(a).locator('[data-lm-resize-handle]');
    const box = (await handle.boundingBox())!;
    const unit = await lm.grid.evaluate(grid => (grid.clientWidth + parseFloat(getComputedStyle(grid).columnGap)) / 3);
    await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
    await page.mouse.down();
    await page.mouse.move(box.x + box.width / 2 + unit, box.y + box.height / 2, { steps: 8 });
    await expect(lm.grid).toHaveClass(/lm-resizing/);
    const active = await lm.grid.locator('.lm-grid-guide-cell').evaluateAll(cells => cells.map(c => c.classList.contains('is-active')));
    expect(active).toEqual([true, true, false]);
    await Promise.all([page.waitForResponse(r => r.url().includes('/lm/resize')), page.mouse.up()]);
    await lm.waitForIdle();
    expect(await lm.cols(a)).toBe(2);
    expect((await lm.storedState()).draft!.layouts![0].blocks![0].cols).toBe(2);
  });

  test('a cancelled or zero-length resize drag sends nothing and restores the span', async ({ page }) => {
    const lm = new LayoutPage(page);
    await lm.plant(layoutOf(['short', 'short']));
    await lm.goto();
    await lm.edit();
    const [a] = await lm.order();

    await lm.dragResize(a, 1.2, { cancel: true });
    await lm.settle();
    expect(await lm.cols(a)).toBe(1);
    await expect(lm.grid).not.toHaveClass(/lm-resizing/);
    await lm.dragResize(a, 0.3);
    await lm.settle();
    expect(await lm.cols(a)).toBe(1);
    expect(lm.requests.filter(r => r.includes('/lm/resize'))).toEqual([]);
  });

  test('S12: the drag handle can make a block as wide as the + button can', async ({ page }) => {
    const lm = new LayoutPage(page);
    await lm.plant(layoutOf(['short', 'short']));
    await lm.goto();
    await lm.edit();
    const [, b] = await lm.order();
    expect((await lm.startColumns())[b], 'b starts in the second column').toBe(1);

    // Drag well past the right edge of the grid.
    await Promise.all([page.waitForResponse(r => r.url().includes('/lm/resize')), lm.dragResize(b, 4)]);
    await lm.waitForIdle();
    const byDrag = await lm.cols(b);
    await lm.toggleFull(b);
    await lm.toggleFull(b);
    await lm.grow(b);
    await lm.grow(b);
    const byButton = await lm.cols(b);
    expect(byButton).toBe(3);

    expect(byDrag, 'widest span the drag allows').toBe(byButton);
  });

  test('pages with resizing or reordering turned off render none of those controls', async ({ page }) => {
    const noResize = new LayoutPage(page, 'noresize');
    await noResize.plant(layoutOf(['short']));
    await noResize.goto();
    await noResize.edit();
    const [r] = await noResize.order();
    await expect(noResize.block(r).locator('[data-lm-resize-handle]')).toHaveCount(0);
    await expect(noResize.control(r, 'Increase size')).toHaveCount(0);
    await expect(page.locator('.lm-grid-guide')).toHaveCount(0);
    await expect(noResize.control(r, 'Move block right')).toHaveCount(1);

    const noReorder = new LayoutPage(page, 'noreorder');
    await noReorder.plant(layoutOf(['short', 'short']));
    await noReorder.goto();
    await noReorder.edit();
    const [x, y] = await noReorder.order();
    await expect(noReorder.block(x).locator('.lm-block-handle')).toHaveCount(0);
    await expect(noReorder.control(x, 'Move block right')).toHaveCount(0);
    await noReorder.drag(x, y, { from: noReorder.block(x).locator('.lm-block-controls') });
    await noReorder.settle();
    expect(await noReorder.order()).toEqual([x, y]);
    expect(noReorder.requests.filter(req => req.includes('/lm/reorder'))).toEqual([]);
  });
});
