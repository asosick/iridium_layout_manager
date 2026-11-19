import { test, expect, type Page } from './fixtures';
import { LayoutPage, layoutOf, PANEL } from './support/layout';

/** The space the page puts between blocks, across and down. */
const gaps = (lm: LayoutPage) => lm.root.evaluate(root => {
  const grid = root.querySelector('[data-lm-grid]')!;
  const guide = grid.querySelector('.lm-grid-guide');
  return {
    rows: getComputedStyle(grid).rowGap,
    columns: getComputedStyle(grid).columnGap,
    guide: guide ? getComputedStyle(guide).columnGap : null,
  };
});

/** How far below the page header its content (a layout page's grid) starts. */
const belowHeader = (page: Page) => page.evaluate(() => {
  const header = document.querySelector('.ir-panel-page-header')!.getBoundingClientRect();
  const content = (document.querySelector('[data-lm-grid]') ?? document.querySelector('.ir-panel-page-content'))!;
  return Math.round(content.getBoundingClientRect().top - header.bottom);
});

test("the grid sits as far below the header as any page's content", async ({ page }) => {
  await page.goto(`${PANEL}/elsewhere`);
  await expect(page.locator('[data-e2e-elsewhere]')).toBeVisible();
  const plain = await belowHeader(page);

  const lm = new LayoutPage(page);
  await lm.plant(layoutOf(['short']));
  await lm.goto();
  expect(await belowHeader(page)).toBe(plain);
  await lm.edit();
  expect(await belowHeader(page), 'in edit mode, with the toolbar on two rows').toBe(plain);
});

test("Iridium's compact vertical padding tightens the layout's own spacing", async ({ page }) => {
  const normal = new LayoutPage(page);
  await normal.plant(layoutOf(['short', 'short', 'short', 'short']));
  await normal.goto();
  expect(await gaps(normal)).toEqual({ rows: '16px', columns: '16px', guide: '16px' });
  const normalBelow = await belowHeader(page);

  const compact = new LayoutPage(page, 'compact');
  await compact.plant(layoutOf(['short', 'short', 'short', 'short']));
  await compact.goto();
  await expect(compact.root).toHaveAttribute('data-vertical-padding', 'compact');
  expect(await gaps(compact), 'the same across a row as down it, and the resize guide matches')
    .toEqual({ rows: '8px', columns: '8px', guide: '8px' });
  expect(await belowHeader(page), 'and the space below the header').toBeLessThan(normalBelow);

  // Masonry measures with the tighter gap: the second row starts 8px below
  // the first, and the second block 8px right of the first.
  const [a, b, , d] = await compact.rects();
  expect(Math.round(d.y - (a.y + a.height))).toBe(8);
  expect(Math.round(b.x - (a.x + a.width))).toBe(8);
});
