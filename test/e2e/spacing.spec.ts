import { test, expect } from './fixtures';
import { LayoutPage, layoutOf } from './support/layout';

/** The space the page puts below its header and between rows of blocks. */
const gaps = (lm: LayoutPage) => lm.root.evaluate(root => ({
  page: getComputedStyle(root).rowGap,
  rows: getComputedStyle(root.querySelector('[data-lm-grid]')!).rowGap,
}));

test("Iridium's compact vertical padding tightens the layout's own spacing", async ({ page }) => {
  const normal = new LayoutPage(page);
  await normal.plant(layoutOf(['short', 'short', 'short', 'short']));
  await normal.goto();
  expect(await gaps(normal)).toEqual({ page: '24px', rows: '16px' });

  const compact = new LayoutPage(page, 'compact');
  await compact.plant(layoutOf(['short', 'short', 'short', 'short']));
  await compact.goto();
  await expect(compact.root).toHaveAttribute('data-vertical-padding', 'compact');
  expect(await gaps(compact)).toEqual({ page: '12px', rows: '8px' });

  // Masonry measures with the tighter gap: the second row starts 8px below
  // the first.
  const [a, , , d] = await compact.rects();
  expect(Math.round(d.y - (a.y + a.height))).toBe(8);
});
