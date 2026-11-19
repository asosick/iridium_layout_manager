import { test, expect } from './fixtures';
import { LayoutPage } from './support/layout';

/** Where the toolbar's rows sit in the page header: their edges and middles. */
const rows = (lm: LayoutPage) => lm.root.evaluate(root => {
  const box = (selector: string) => {
    const r = root.querySelector(selector)!.getBoundingClientRect();
    return { left: Math.round(r.left), right: Math.round(r.right), middle: Math.round(r.top + r.height / 2) };
  };
  return {
    header: box('.ir-panel-page-header'),
    pages: box('#lm-selectors'),
    controls: box('.lm-toolbar > .lm-controls'),
  };
});

/** Whether the page buttons and the controls share a row: the buttons are
 *  shorter than the controls, and centred on them. */
const oneRow = (a: { middle: number }, b: { middle: number }) => Math.abs(a.middle - b.middle) <= 2;

test.describe('toolbar', () => {
  // Narrow enough that the toolbar, kept to half the header beside a title,
  // wraps onto two rows in edit mode.
  test.use({ viewport: { width: 1100, height: 800 } });

  test('beside a title, each of its rows keeps to the right edge', async ({ page }) => {
    const lm = new LayoutPage(page, 'pages');
    await lm.goto();
    await lm.edit();
    const { header, pages, controls } = await rows(lm);
    expect(pages.middle, 'the pages above the controls').toBeLessThan(controls.middle - 20);
    expect(pages.right).toBe(header.right);
    expect(controls.right).toBe(header.right);
  });

  test('with no title it keeps to the left, on one row', async ({ page }) => {
    const lm = new LayoutPage(page, 'untitled');
    await lm.goto();
    await lm.edit();
    const { header, pages, controls } = await rows(lm);
    expect(pages.left).toBe(header.left);
    expect(oneRow(pages, controls), "the whole header is the toolbar's: one row").toBe(true);
    expect(controls.right).toBeLessThan(header.right);
  });

  test('with no title and AlignToolbarRight it keeps to the right, on one row', async ({ page }) => {
    const lm = new LayoutPage(page, 'right');
    await lm.goto();
    const view = await rows(lm);
    expect(view.controls.right, 'Edit, in view mode').toBe(view.header.right);
    await lm.edit();
    const { header, pages, controls } = await rows(lm);
    expect(controls.right).toBe(header.right);
    expect(oneRow(pages, controls)).toBe(true);
    expect(pages.left).toBeGreaterThan(header.left);
  });
});
