import { test, expect } from './fixtures';
import { LayoutPage, layoutOf } from './support/layout';

/**
 * Drag to reorder, with real pointer input through the browser's native drag
 * and drop (SortableJS). After every drop the DOM order, the order a user
 * reads on screen, the stored order and a reload must agree.
 */

async function expectAgreed(lm: LayoutPage, expected: string[]) {
  expect(await lm.order(), 'DOM order').toEqual(expected);
  expect(await lm.visualOrder(), 'order on screen').toEqual(expected);
  expect(await lm.serverOrder(), 'stored order').toEqual(expected);
}

test.describe('drag to reorder', () => {
  test('equal-height blocks in one column: first to last, last to first, a middle swap', async ({ page }) => {
    const lm = new LayoutPage(page, 'narrow');
    await lm.plant(layoutOf(['short', 'short', 'short', 'short']));
    await lm.goto();
    await lm.edit();
    const [a, b, c, d] = await lm.order();

    await lm.dragOnto(a, d);
    await expectAgreed(lm, [b, c, d, a]);
    await lm.dragOnto(a, b);
    await expectAgreed(lm, [a, b, c, d]);
    await lm.dragOnto(b, c);
    await expectAgreed(lm, [a, c, b, d]);

    await lm.done();
    await lm.reload();
    expect(await lm.order()).toEqual([a, c, b, d]);
  });

  test('equal-height blocks in three columns: across rows and within a row', async ({ page }) => {
    const lm = new LayoutPage(page);
    await lm.plant(layoutOf(['short', 'short', 'short', 'short', 'short', 'short']));
    await lm.goto();
    await lm.edit();
    const [a, b, c, d, e, f] = await lm.order();

    await lm.dragOnto(a, f);
    await expectAgreed(lm, [b, c, d, e, f, a]);
    await lm.dragOnto(a, b);
    await expectAgreed(lm, [a, b, c, d, e, f]);
    await lm.dragOnto(b, e);
    await expectAgreed(lm, [a, c, d, e, b, f]);
  });

  test('S1: with mixed heights and spans a drop leaves the screen order equal to the stored order', async ({ page }) => {
    const lm = new LayoutPage(page);
    // A tall block, a two-column block beside it, then two short ones.
    await lm.plant(layoutOf(['tall', ['short', 2], 'short', 'short']));
    await lm.goto();
    await lm.edit();
    const [tall, wide, a, b] = await lm.order();
    await expectAgreed(lm, [tall, wide, a, b]);

    // Drop b on the wide block's first column, as a person moving it up a
    // row would.
    const box = (await lm.block(wide).boundingBox())!;
    const reorder = page.waitForResponse(r => r.url().includes('/lm/reorder'));
    const released = await lm.drag(b, { x: box.x + box.width / 4, y: box.y + box.height / 2 });
    await reorder;
    await lm.waitForIdle();
    expect(await lm.order()).toEqual([tall, b, wide, a]);
    expect(await lm.serverOrder()).toEqual([tall, b, wide, a]);

    expect(await lm.visualOrder(), 'order on screen').toEqual(await lm.order());
    const dropped = (await lm.block(b).boundingBox())!;
    expect(released.x >= dropped.x && released.x <= dropped.x + dropped.width && released.y >= dropped.y && released.y <= dropped.y + dropped.height,
      'the dropped block sits where it was released').toBe(true);
  });

  test('S2: a second drag while the first reorder is in flight is kept', async ({ page }) => {
    const lm = new LayoutPage(page, 'narrow');
    await lm.plant(layoutOf(['short', 'short', 'short', 'short']));
    await lm.goto();
    await lm.edit();
    const [a, b, c, d] = await lm.order();
    // Hold the first reorder on the network until the second drop is made.
    let answer!: () => void;
    const held = new Promise<void>(resolve => { answer = resolve; });
    await page.route('**/lm/reorder**', async route => {
      await held;
      await route.continue();
    });

    await lm.drag(a, d);
    expect(await lm.order()).toEqual([b, c, d, a]);
    await lm.drag(d, b);
    expect(await lm.order(), 'the second drop shows').toEqual([d, b, c, a]);
    answer();
    await lm.settle();

    await expectAgreed(lm, [d, b, c, a]);
    expect(lm.requests.filter(r => r.includes('/lm/reorder')), 'both drops were sent, in turn').toHaveLength(2);
  });

  test.describe('while another edit is in flight', () => {
    test('S3/S5: a drag made while a resize is in flight is kept, and so is the resize', async ({ page }) => {
      const lm = new LayoutPage(page);
      // Two slow blocks: every edit takes the server 600ms or more, as a
      // page of real widgets would.
      await lm.plant(layoutOf(['short', 'short', 'short', 'slow', 'slow']));
      await lm.goto();
      await lm.edit();
      const [x, y, z, s1, s2] = await lm.order();
      const pageErrors: string[] = [];
      page.on('pageerror', error => pageErrors.push(error.message));

      await lm.control(x, 'Increase size').click();
      await lm.drag(z, x);
      await page.waitForTimeout(1500);
      await lm.waitForIdle();
      expect(await lm.cols(x), 'the resize landed').toBe(2);

      expect(pageErrors, 'uncaught page errors').toEqual([]);
      expect(lm.requests.filter(r => r.includes('/lm/reorder')), 'the drop sent a reorder').toHaveLength(1);
      expect(await lm.order(), 'DOM order').toEqual([z, x, y, s1, s2]);
      expect(await lm.serverOrder(), 'stored order').toEqual([z, x, y, s1, s2]);
    });
  });

  test('S6: in view mode no block can be dragged, from anywhere', async ({ page }) => {
    const lm = new LayoutPage(page);
    await lm.plant(layoutOf(['short', 'short', 'short']));
    await lm.goto();
    const [a, , c] = await lm.order();
    await expect(lm.block(a).locator('.lm-block-handle')).toBeHidden();

    await lm.drag(a, c, { from: lm.block(a).locator('[data-e2e-kind]') });
    await lm.settle();
    expect(await lm.order()).toEqual([a, (await lm.order())[1], c]);
    expect(lm.requests.filter(r => r.includes('/lm/reorder'))).toEqual([]);
  });

  test('S6: in edit mode only the handle drags; content, inputs, nested sortables and draggables inside a block do not', async ({ page }) => {
    const lm = new LayoutPage(page);
    await lm.plant(layoutOf(['controls', 'short', 'short']));
    await lm.goto();
    await lm.edit();
    const [controls, a, b] = await lm.order();

    await lm.drag(a, b, { from: lm.block(a).locator('[data-e2e-kind]') });
    await lm.drag(controls, b, { from: lm.block(controls).locator('[data-e2e-input]') });
    await lm.drag(controls, b, { from: lm.block(controls).locator('[data-e2e-draggable]') });
    // The block's own sortable list still works, and only reorders itself.
    const items = lm.block(controls).locator('[data-e2e-sortlist] li');
    const gamma = (await lm.block(controls).locator('[data-e2e-item="gamma"]').boundingBox())!;
    await lm.drag(controls, { x: gamma.x + gamma.width / 2 + 5, y: gamma.y + gamma.height / 2 }, {
      from: lm.block(controls).locator('[data-e2e-item="alpha"]'),
      steps: 10,
    });
    await lm.settle();

    await expect(items).toHaveText(['beta', 'gamma', 'alpha']);
    expect(await lm.order()).toEqual([controls, a, b]);
    expect(lm.requests.filter(r => r.includes('/lm/reorder'))).toEqual([]);
  });

  test.describe('when the server refuses the reorder', () => {
    test.use({ allowedPageProblems: [/status of 500/] });

    test('S20: a refused reorder is reported and the screen goes back to the stored order', async ({ page }) => {
      const lm = new LayoutPage(page, 'narrow');
      await lm.plant(layoutOf(['short', 'short', 'short']));
      await lm.goto();
      await lm.edit();
      const [a, b, c] = await lm.order();
      await page.route('**/lm/reorder**', route => route.fulfill({ status: 500, contentType: 'text/plain', body: 'An unexpected error occurred on our end. Please try again later.' }));

      await lm.drag(a, c);
      await lm.settle();
      expect(await lm.serverOrder(), 'stored order').toEqual([a, b, c]);

      await expect(page.getByText('An unexpected error occurred')).toBeVisible({ timeout: 2_000 });
      expect(await lm.order(), 'order on screen').toEqual([a, b, c]);
    });
  });

  test('S18: after a drag the block controls still work', async ({ page }) => {
    const lm = new LayoutPage(page, 'narrow');
    await lm.plant(layoutOf(['short', 'short', 'short']));
    await lm.goto();
    await lm.edit();
    const [a, b, c] = await lm.order();
    await lm.dragOnto(a, c);
    expect(await lm.order()).toEqual([b, c, a]);

    await lm.control(b, 'Remove block').click();
    await lm.settle();
    expect(lm.requests.filter(r => r.includes('/lm/remove')), 'Remove sent a request').toHaveLength(1);
    expect(await lm.order()).toEqual([c, a]);
  });
});
