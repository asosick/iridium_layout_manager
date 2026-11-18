import { test, expect } from './fixtures';
import { LayoutPage, openEditor } from './support/layout';

test.describe('add and remove', () => {
  test('every kind of block can be added and renders its own content', async ({ page }) => {
    const lm = await openEditor(page);
    for (const kind of ['short', 'tall', 'grow', 'slow', 'controls']) {
      const id = await lm.addBlock(kind);
      await expect(lm.block(id).locator(`[data-e2e-kind="${kind}"]`)).toHaveAttribute('data-e2e-id', id);
    }
    const table = await lm.addBlock('table');
    await expect(lm.block(table).locator('tbody tr')).toHaveCount(3);
    const chart = await lm.addBlock('chart');
    await expect(lm.block(chart).locator('.apexcharts-canvas')).toBeVisible();
    const stats = await lm.addBlock('stats');
    await expect(lm.block(stats).getByText('Revenue', { exact: true })).toBeVisible();

    await lm.done();
    await lm.reload();
    expect(await lm.order()).toHaveLength(8);
    expect(await lm.serverOrder()).toEqual(await lm.order());
  });

  test('two blocks of the same type are separate instances', async ({ page }) => {
    const lm = await openEditor(page);
    const first = await lm.addBlock('short');
    const second = await lm.addBlock('short');
    expect(first).not.toEqual(second);
    await expect(lm.block(second).locator('[data-e2e-kind="short"]')).toHaveAttribute('data-e2e-id', second);

    await lm.remove(first);
    expect(await lm.order()).toEqual([second]);
    await lm.done();
    await lm.reload();
    expect(await lm.order()).toEqual([second]);
  });

  test('a double-clicked remove removes that one block', async ({ page }) => {
    const lm = await openEditor(page);
    const [a, b, c] = await lm.addBlocks('short', 'short', 'short');
    await lm.control(b, 'Remove block').dblclick();
    await lm.settle();
    expect(await lm.order()).toEqual([a, c]);
    expect(await lm.serverOrder()).toEqual([a, c]);
  });

  test('removing the last block brings back the empty state', async ({ page }) => {
    const lm = await openEditor(page);
    const id = await lm.addBlock('short');
    await expect(page.locator('.lm-empty')).toBeHidden();
    await lm.remove(id);
    await expect(page.locator('.lm-empty')).toBeVisible();
    expect(await lm.serverOrder()).toEqual([]);
  });

  test.describe('past the old cookie ceiling', () => {
    test('S4: a 31st block is added like any other', async ({ page }) => {
      const lm = new LayoutPage(page);
      const blocks = Array.from({ length: 30 }, (_, i) => ({ id: `0000000${i}`.slice(-8) + '-0000-4000-8000-000000000000', type: 'short', cols: 1 }));
      await lm.plant({ layouts: [{ blocks }, { blocks: [] }, { blocks: [] }] });
      await lm.goto();
      await lm.edit();
      expect(await lm.order()).toHaveLength(30);

      await lm.openGallery();
      await Promise.all([
        page.waitForResponse(r => r.url().includes('/lm/add')),
        lm.cardButton('short').click(),
      ]);
      await lm.waitForIdle();
      await expect(page.getByText('An unexpected error occurred')).toHaveCount(0);
      expect(await lm.order()).toHaveLength(31);
    });
  });
});
