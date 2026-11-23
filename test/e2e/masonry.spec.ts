import { test, expect, type Page } from './fixtures';
import { LayoutPage, layoutOf } from './support/layout';

/** Overlapping blocks or content spilling out of its block, if any. */
async function geometryProblems(lm: LayoutPage): Promise<string[]> {
  await lm.frames(3);
  return lm.grid.evaluate(grid => {
    const blocks = [...grid.querySelectorAll<HTMLElement>(':scope > [data-lm-block]')].map(el => ({
      id: el.dataset.id!,
      box: el.getBoundingClientRect(),
      content: el.querySelector('.lm-block-content')!.getBoundingClientRect(),
    }));
    const problems: string[] = [];
    for (const b of blocks) {
      if (b.content.bottom > b.box.bottom + 1) problems.push(`${b.id}: content overflows by ${Math.round(b.content.bottom - b.box.bottom)}px`);
    }
    for (let i = 0; i < blocks.length; i++) {
      for (let j = i + 1; j < blocks.length; j++) {
        const [p, q] = [blocks[i].box, blocks[j].box];
        const w = Math.min(p.right, q.right) - Math.max(p.left, q.left);
        const h = Math.min(p.bottom, q.bottom) - Math.max(p.top, q.top);
        if (w > 1 && h > 1) problems.push(`${blocks[i].id} overlaps ${blocks[j].id} by ${Math.round(w)}x${Math.round(h)}px`);
      }
    }
    return problems;
  });
}

/** Samples every block's box once per painted frame until stopped. */
async function startFrameSampler(page: Page) {
  await page.evaluate(() => {
    const w = window as any;
    w.__frames = [];
    w.__sampling = true;
    const sample = () => {
      const frame: Record<string, number[]> = {};
      document.querySelectorAll<HTMLElement>('[data-lm-grid] > [data-lm-block]').forEach(el => {
        const r = el.getBoundingClientRect();
        frame[el.dataset.id!] = [Math.round(r.left), Math.round(r.top), Math.round(r.height)];
      });
      w.__frames.push(frame);
      // Read after the frame's rAF work (masonry runs in rAF), as painted.
      if (w.__sampling) requestAnimationFrame(() => setTimeout(sample, 0));
    };
    sample();
  });
}

/** For each block: the largest move (px) of its top-left or height seen in any frame. */
async function stopFrameSampler(page: Page): Promise<Record<string, number>> {
  return page.evaluate(() => {
    const w = window as any;
    w.__sampling = false;
    const frames: Record<string, number[]>[] = w.__frames;
    const first = frames[0];
    const drift: Record<string, number> = {};
    for (const id of Object.keys(first)) {
      drift[id] = Math.max(...frames.filter(f => f[id]).map(f => Math.max(...f[id].map((v, i) => Math.abs(v - first[id][i])))));
    }
    return drift;
  });
}

test.describe('masonry', () => {
  test('blocks never overlap or clip their content: on load, add, resize, late growth, window resize, view mode', async ({ page }) => {
    const lm = new LayoutPage(page);
    await lm.plant(layoutOf(['short', 'tall', 'grow', 'short', ['short', 2], 'controls']));
    await lm.goto();
    // In the grid: the gallery's preview of grow is a copy of it.
    const grow = lm.grid.locator('[data-e2e-kind="grow"]');
    await expect(grow).toHaveCSS('height', '400px');
    expect(await geometryProblems(lm), 'view mode after the late growth').toEqual([]);

    await lm.edit();
    expect(await geometryProblems(lm), 'edit mode adds the control bars').toEqual([]);
    await lm.addBlock('tall');
    await expect(grow).toHaveCSS('height', '400px');
    expect(await geometryProblems(lm), 'after an add').toEqual([]);
    const [first] = await lm.order();
    await lm.grow(first);
    await expect(grow).toHaveCSS('height', '400px');
    expect(await geometryProblems(lm), 'after a resize').toEqual([]);

    await page.setViewportSize({ width: 900, height: 900 });
    expect(await geometryProblems(lm), 'after the window narrowed').toEqual([]);
    await lm.done();
    expect(await geometryProblems(lm), 'back in view mode').toEqual([]);
  });

  test('S5: an edit to one block leaves the other blocks where they are, in every frame', async ({ page }, testInfo) => {
    const lm = new LayoutPage(page);
    // grow renders at 80px and becomes 400px after it is inserted, like a
    // chart or image that loads late.
    await lm.plant(layoutOf(['grow', 'short', 'short', 'short', 'short']));
    await lm.goto();
    await lm.edit();
    await expect(lm.grid.locator('[data-e2e-kind="grow"]')).toHaveCSS('height', '400px');
    const [grow, a, b, c, last] = await lm.order();

    await startFrameSampler(page);
    await lm.grow(last);
    await page.waitForTimeout(800);
    const drift = await stopFrameSampler(page);
    await testInfo.attach('largest drift per block (px)', { body: JSON.stringify(drift, null, 2), contentType: 'application/json' });

    expect({ [grow]: drift[grow], [a]: drift[a], [b]: drift[b], [c]: drift[c] }).toEqual({ [grow]: 0, [a]: 0, [b]: 0, [c]: 0 });
  });

  test('a popover over the blocks below neither pushes them down nor leaves a gap once it closes, and draws over them', async ({ page }) => {
    // One column, so the short block sits under the popover block.
    await page.setViewportSize({ width: 480, height: 900 });
    const lm = new LayoutPage(page);
    await lm.plant(layoutOf(['popover', 'short']));
    await lm.goto();
    const [top, below] = ['popover-1', 'short-2'];
    const panel = lm.block(top).locator('[data-e2e-popover-panel]');
    const belowTop = async () => (await lm.block(below).boundingBox())!.y;
    const gap = async () => {
      await lm.frames(3);
      return (await belowTop()) - (await lm.block(top).boundingBox())!.y
        - (await lm.block(top).locator('.lm-block-content').boundingBox())!.height;
    };
    const closedGap = await gap();
    const startTop = await belowTop();

    await lm.block(top).locator('[data-e2e-popover-toggle]').click();
    await expect(panel).toBeVisible();
    // The widget grows a line while its popover is open, as a table does when
    // a column is hidden or shown: the block measures itself again.
    await lm.block(top).locator('[data-e2e-popover-add]').click();
    await expect(lm.block(top).locator('.e2e-popover-line')).toHaveCount(1);
    expect(await gap(), 'the open popover takes no room').toBeLessThanOrEqual(closedGap + 8);
    expect(await belowTop() - startTop, 'the block below moves by the added line only').toBeLessThanOrEqual(20 + 8);

    // The panel reaches over the block below and draws over it.
    const box = (await lm.block(below).boundingBox())!;
    const onTop = await page.evaluate(([x, y]) => !!document.elementFromPoint(x, y)?.closest('[data-e2e-popover-panel]'),
      [box.x + 20, box.y + 20]);
    expect(onTop, 'the panel draws over the next block').toBe(true);

    await lm.block(top).locator('[data-e2e-popover-toggle]').click();
    await expect(panel).toBeHidden();
    expect(await gap(), 'no gap once it closes').toBeLessThanOrEqual(closedGap + 8);
  });
});
