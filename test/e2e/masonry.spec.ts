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
    const grow = page.locator('[data-e2e-kind="grow"]');
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
    await expect(page.locator('[data-e2e-kind="grow"]')).toHaveCSS('height', '400px');
    const [grow, a, b, c, last] = await lm.order();

    await startFrameSampler(page);
    await lm.grow(last);
    await page.waitForTimeout(800);
    const drift = await stopFrameSampler(page);
    await testInfo.attach('largest drift per block (px)', { body: JSON.stringify(drift, null, 2), contentType: 'application/json' });

    test.fail(true, 'S5: every edit re-renders every block; late content (grow) collapses and regrows, moving the blocks below it');
    expect({ [grow]: drift[grow], [a]: drift[a], [b]: drift[b], [c]: drift[c] }).toEqual({ [grow]: 0, [a]: 0, [b]: 0, [c]: 0 });
  });
});
