import { test, expect, type Locator } from './fixtures';
import { LayoutPage, layoutOf } from './support/layout';

/** The page settings dialog (Settings): a page's columns and spacing. */
test.describe('settings', () => {
  const dialog = (lm: LayoutPage) => lm.page.locator('[data-lm-settings]');
  const toggle = (lm: LayoutPage) => lm.page.locator('[data-lm-settings-toggle]');
  const apply = (lm: LayoutPage) => dialog(lm).locator('[data-lm-settings-apply]');
  const radio = (lm: LayoutPage, name: string) => dialog(lm).getByRole('radio', { name, exact: true });
  /** The label a user clicks: the radio itself is out of sight. */
  const choice = (lm: LayoutPage, text: string): Locator =>
    dialog(lm).locator('label.lm-segment').filter({ hasText: new RegExp(`^\\s*${text}\\s*$`) });
  const gap = (lm: LayoutPage) => lm.grid.evaluate(grid => getComputedStyle(grid).columnGap);

  const open = async (lm: LayoutPage) => {
    await toggle(lm).click();
    await expect(dialog(lm)).toBeVisible();
  };

  /** Opens the dialog, makes the choices given and applies them. */
  const choose = async (lm: LayoutPage, choices: { columns?: string; spacing?: string }) => {
    await open(lm);
    if (choices.columns) await choice(lm, choices.columns).click();
    if (choices.spacing) await choice(lm, choices.spacing).click();
    await Promise.all([
      lm.page.waitForResponse(r => r.url().includes('/lm/settings')),
      apply(lm).click(),
    ]);
    await expect(dialog(lm)).toBeHidden();
    await lm.waitForIdle();
  };

  test("Apply lays the page out as chosen, and Done keeps it", async ({ page }) => {
    const lm = new LayoutPage(page, 'settings');
    await lm.plant(layoutOf(['short', 'short', ['short', 3]], ['tall']));
    await lm.goto();
    await expect(toggle(lm), 'only in edit mode').toBeHidden();
    await lm.edit();

    // It opens on what the page has, with the columns focused.
    await open(lm);
    await expect(radio(lm, '3')).toBeChecked();
    await expect(radio(lm, 'Normal')).toBeChecked();
    await expect(radio(lm, '3')).toBeFocused();
    await expect(apply(lm), 'nothing to apply yet').toBeDisabled();
    await page.keyboard.press('Escape');
    await expect(dialog(lm)).toBeHidden();

    await choose(lm, { columns: '5', spacing: 'Compact' });
    expect(await lm.columnCount()).toBe(5);
    expect(await gap(lm)).toBe('8px');
    expect(await lm.cols('short-1'), 'a block keeps its span').toBe(1);
    expect(await lm.cols('short-3'), 'a full-width block stays full width').toBe(5);

    // The other page keeps its own.
    await lm.selectPage(1);
    expect(await lm.columnCount()).toBe(3);
    expect(await gap(lm)).toBe('16px');
    await lm.selectPage(0);

    await lm.done();
    await lm.reload();
    expect(await lm.columnCount()).toBe(5);
    expect(await gap(lm)).toBe('8px');
  });

  test('no spacing at all: blocks touch, across a row and down it', async ({ page }) => {
    const lm = new LayoutPage(page, 'settings');
    await lm.plant(layoutOf(['short', 'short', 'short']));
    await lm.goto();
    await lm.edit();
    await choose(lm, { columns: '2', spacing: 'None' });
    expect(await gap(lm)).toBe('0px');
    // Masonry measures with no gap: the third block starts where the first
    // ends, and the second where the first ends across.
    const [a, b, c] = await lm.rects();
    expect(Math.round(b.x - (a.x + a.width))).toBe(0);
    expect(Math.abs(Math.round(c.y - (a.y + a.height)))).toBeLessThanOrEqual(8);
  });

  test('Cancel, Escape and a click beside the dialog change nothing; the page keys wait', async ({ page }) => {
    const lm = new LayoutPage(page, 'settings');
    await lm.plant(layoutOf(['short'], ['tall']));
    await lm.goto();
    await lm.edit();
    const sent = () => lm.requests.filter(r => /\/lm\/(settings|select)/.test(r));
    const before = sent().length;

    await open(lm);
    await choice(lm, '2').click();
    await expect(apply(lm)).toBeEnabled();
    await dialog(lm).getByRole('button', { name: 'Cancel', exact: true }).click();
    await expect(dialog(lm)).toBeHidden();

    // It opens again on what the page has, not on the choice cancelled.
    await open(lm);
    await expect(radio(lm, '3')).toBeChecked();
    // A page key pressed in the dialog does not switch the page behind it.
    await dialog(lm).getByRole('button', { name: 'Cancel', exact: true }).focus();
    await page.keyboard.press('2');
    await page.keyboard.press('Escape');
    await expect(dialog(lm)).toBeHidden();

    await open(lm);
    await page.mouse.click(4, 4);
    await expect(dialog(lm)).toBeHidden();

    await lm.settle();
    expect(sent().slice(before), 'nothing sent').toEqual([]);
    expect(await lm.currentLayout()).toBe(0);
    expect(await lm.columnCount()).toBe(3);
  });

  test('Reset and Cancel undo the settings with the other edits', async ({ page }) => {
    const lm = new LayoutPage(page, 'settings');
    await lm.plant(layoutOf(['short', 'short']));
    await lm.goto();
    await lm.edit();
    await choose(lm, { columns: '4', spacing: 'Tight' });
    expect(await lm.columnCount()).toBe(4);
    expect(await gap(lm)).toBe('4px');
    await lm.reset();
    expect(await lm.columnCount()).toBe(3);
    expect(await gap(lm)).toBe('16px');

    await choose(lm, { columns: '2' });
    await lm.cancel();
    expect(await lm.columnCount()).toBe(3);
    const { committed } = await lm.storedState();
    expect(committed?.layouts?.[0]).not.toHaveProperty('columns');
  });

  test('a page without Settings has no settings', async ({ page }) => {
    const lm = new LayoutPage(page);
    await lm.goto();
    await lm.edit();
    await expect(toggle(lm)).toHaveCount(0);
    await expect(dialog(lm)).toHaveCount(0);
  });
});
