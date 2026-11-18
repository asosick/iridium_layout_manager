import { test, expect } from './fixtures';
import { LayoutPage, layoutOf } from './support/layout';

/** The space the page puts below its header and between blocks. */
const gaps = (lm: LayoutPage) => lm.root.evaluate(root => {
  const grid = root.querySelector('[data-lm-grid]')!;
  const guide = grid.querySelector('.lm-grid-guide');
  return {
    page: getComputedStyle(root).rowGap,
    rows: getComputedStyle(grid).rowGap,
    columns: getComputedStyle(grid).columnGap,
    guide: guide ? getComputedStyle(guide).columnGap : null,
  };
});

test("Iridium's compact vertical padding tightens the layout's own spacing", async ({ page }) => {
  const normal = new LayoutPage(page);
  await normal.plant(layoutOf(['short', 'short', 'short', 'short']));
  await normal.goto();
  expect(await gaps(normal)).toEqual({ page: '24px', rows: '16px', columns: '16px', guide: '16px' });

  const compact = new LayoutPage(page, 'compact');
  await compact.plant(layoutOf(['short', 'short', 'short', 'short']));
  await compact.goto();
  await expect(compact.root).toHaveAttribute('data-vertical-padding', 'compact');
  expect(await gaps(compact), 'the same across a row as down it, and the resize guide matches')
    .toEqual({ page: '12px', rows: '8px', columns: '8px', guide: '8px' });

  // Masonry measures with the tighter gap: the second row starts 8px below
  // the first, and the second block 8px right of the first.
  const [a, b, , d] = await compact.rects();
  expect(Math.round(d.y - (a.y + a.height))).toBe(8);
  expect(Math.round(b.x - (a.x + a.width))).toBe(8);
});
