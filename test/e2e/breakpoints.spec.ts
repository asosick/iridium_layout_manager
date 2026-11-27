import { test, expect } from './fixtures';
import { LayoutPage, layoutOf } from './support/layout';

/**
 * Columns per breakpoint: the "responsive" page has one column on phones, two
 * from md (768px) and four from lg (1024px), with the settings dialog.
 */
test.describe('columns per breakpoint', () => {
  const dialog = (lm: LayoutPage) => lm.page.locator('[data-lm-settings]');
  const breakpoints = (lm: LayoutPage) => dialog(lm).locator('[data-lm-settings-breakpoints]');
  const select = (lm: LayoutPage, bp: string) => dialog(lm).locator(`select[name="lm-columns-${bp}"]`);
  const row = (lm: LayoutPage, bp: string) => dialog(lm).locator(`.lm-breakpoint[data-breakpoint="${bp}"]`);
  const columnsChoice = (lm: LayoutPage, n: string) =>
    dialog(lm).locator('label.lm-segment').filter({ hasText: new RegExp(`^\\s*${n}\\s*$`) });

  /** How many of the columns on screen block `id` covers. */
  const shownSpan = (lm: LayoutPage, id: string) => lm.block(id).evaluate(block => {
    const grid = block.parentElement!;
    const styles = getComputedStyle(grid);
    const columns = styles.gridTemplateColumns.split(' ').length;
    const gap = parseFloat(styles.columnGap) || 0;
    const unit = (grid.clientWidth + gap) / columns;
    return Math.round((block.getBoundingClientRect().width + gap) / unit);
  });

  const openSettings = async (lm: LayoutPage) => {
    await lm.page.locator('[data-lm-settings-toggle]').click();
    await expect(dialog(lm)).toBeVisible();
  };

  test('each breakpoint has its own columns, and blocks keep their share of the page', async ({ page }) => {
    const lm = new LayoutPage(page, 'responsive');
    await lm.plant(layoutOf([['short', 2], ['short', 3], 'short', ['short', 4]]));

    await page.setViewportSize({ width: 1280, height: 900 });
    await lm.goto();
    expect(await lm.columnCount(), 'a laptop').toBe(4);
    expect(await Promise.all(['short-1', 'short-2', 'short-3', 'short-4'].map(id => shownSpan(lm, id)))).toEqual([2, 3, 1, 4]);

    await page.setViewportSize({ width: 800, height: 900 });
    await expect.poll(() => lm.columnCount(), 'a tablet').toBe(2);
    // Half of four is one of two; three of four rounds up to all of two.
    expect(await Promise.all(['short-1', 'short-2', 'short-3', 'short-4'].map(id => shownSpan(lm, id)))).toEqual([1, 2, 1, 2]);
    expect(await lm.cols('short-1'), 'the stored span is unchanged').toBe(2);

    await page.setViewportSize({ width: 375, height: 812 });
    await expect.poll(() => lm.columnCount(), 'a phone').toBe(1);
  });

  test('the settings choose the columns for each screen size', async ({ page }) => {
    const lm = new LayoutPage(page, 'responsive');
    await lm.plant(layoutOf(['short', ['short', 4]]));
    await page.setViewportSize({ width: 800, height: 900 });
    await lm.goto();
    await lm.edit();

    await openSettings(lm);
    await expect(breakpoints(lm), 'closed while the page has GridColumns').not.toHaveAttribute('open', '');
    await breakpoints(lm).locator('summary').click();
    await expect(row(lm, 'md')).toHaveClass(/\bis-current\b/);
    await expect(row(lm, 'md').getByText('This screen')).toBeVisible();
    await expect(row(lm, 'lg').getByText('This screen')).toBeHidden();
    for (const [bp, n] of [['xs', '1'], ['sm', '1'], ['md', '2'], ['lg', '4'], ['xl', '4'], ['2xl', '4']]) {
      await expect(select(lm, bp), bp).toHaveValue(n);
    }

    // A number of columns changes the widest screens and keeps the others.
    await columnsChoice(lm, '6').click();
    await expect(select(lm, 'lg')).toHaveValue('6');
    await expect(select(lm, 'md')).toHaveValue('2');

    // A screen size's own columns.
    await select(lm, 'md').selectOption('3');
    await expect(dialog(lm).getByRole('radio', { name: '6', exact: true }), 'the widest is still 6').toBeChecked();
    await Promise.all([
      page.waitForResponse(r => r.url().includes('/lm/settings')),
      dialog(lm).locator('[data-lm-settings-apply]').click(),
    ]);
    await expect(dialog(lm)).toBeHidden();
    await lm.waitForIdle();
    expect(await lm.columnCount(), 'this tablet').toBe(3);
    expect(await lm.cols('short-2'), 'a full-width block stays full width').toBe(6);
    expect(await shownSpan(lm, 'short-2')).toBe(3);

    // The dialog opens on the screen sizes chosen.
    await openSettings(lm);
    await expect(breakpoints(lm)).toHaveAttribute('open', '');
    await expect(select(lm, 'md')).toHaveValue('3');
    await page.keyboard.press('Escape');

    // Done keeps them.
    await lm.done();
    await lm.reload();
    expect(await lm.columnCount()).toBe(3);
    await page.setViewportSize({ width: 1280, height: 900 });
    await expect.poll(() => lm.columnCount(), 'a laptop').toBe(6);
  });

  test('a block resized on a narrower screen is stored in the widest columns', async ({ page }) => {
    const lm = new LayoutPage(page, 'responsive');
    await lm.plant(layoutOf(['short', 'short']));
    await page.setViewportSize({ width: 800, height: 900 });
    await lm.goto();
    await lm.edit();

    // One of four shows as one of two; one more column on screen is all of
    // it, so the block is full width everywhere.
    await lm.resizeTo('short-1', 4);
    expect(await shownSpan(lm, 'short-1')).toBe(2);
    expect((await lm.storedState()).draft?.layouts?.[0]?.blocks?.[0]?.cols).toBe(4);

    await page.setViewportSize({ width: 1280, height: 900 });
    await expect.poll(() => shownSpan(lm, 'short-1')).toBe(4);
  });
});
