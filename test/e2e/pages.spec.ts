import { test, expect } from './fixtures';
import { LayoutPage, layoutOf } from './support/layout';

/** The numbered pages (layouts) a user can flip between. */
test.describe('pages', () => {
  test('view mode shows only populated page numbers; edit mode shows every page', async ({ page }) => {
    const lm = new LayoutPage(page);
    await lm.plant(layoutOf(['short'], [], ['tall']));
    await lm.goto();
    await expect(lm.pageButton(0)).toBeVisible();
    await expect(lm.pageButton(1)).toBeHidden();
    await expect(lm.pageButton(2)).toBeVisible();
    await lm.edit();
    for (const i of [0, 1, 2]) await expect(lm.pageButton(i)).toBeVisible();

    const single = new LayoutPage(page);
    await single.plant(layoutOf(['short'], [], []));
    await single.goto();
    await expect(page.locator('#lm-selectors'), 'one populated page: no strip').toBeHidden();
  });

  test('a page number and the 1..9 keys show that page; Add lands on the page shown', async ({ page }) => {
    const lm = new LayoutPage(page);
    const planted = layoutOf(['short'], ['tall'], []);
    await lm.plant(planted);
    await lm.goto();
    const [onFirst] = await lm.order();

    await lm.selectPage(1);
    expect(await lm.order()).toEqual(['tall-2']);
    await expect(lm.pageButton(1)).toHaveClass(/ir-btn-primary/);
    await expect(lm.pageButton(0)).toHaveClass(/ir-btn-outline/);

    await lm.edit();
    await page.locator('h1').first().click();
    await Promise.all([page.waitForResponse(r => r.url().includes('/lm/select')), page.keyboard.press('3')]);
    await lm.waitForIdle();
    expect(await lm.currentLayout()).toBe(2);
    const added = await lm.addBlock('short');
    expect(await lm.serverOrder(2)).toEqual([added]);
    expect(await lm.serverOrder(0)).toEqual([onFirst]);
    expect(await lm.serverOrder(1)).toEqual(['tall-2']);
  });

  test('Done on a page emptied while editing moves to a populated page', async ({ page }) => {
    const lm = new LayoutPage(page);
    await lm.plant(layoutOf(['short'], ['tall'], []));
    await lm.goto();
    await lm.selectPage(1);
    await lm.edit();
    await lm.remove('tall-2');
    await expect(page.locator('.lm-empty')).toBeVisible();

    await Promise.all([page.waitForResponse(r => r.url().includes('/lm/select')), lm.doneButton().click()]);
    await lm.waitForIdle();
    expect(await lm.currentLayout()).toBe(0);
    expect(await lm.order()).toEqual(['short-1']);
    await expect(lm.pageButton(0)).toHaveClass(/ir-btn-primary/);
  });

  test('S7: a reload keeps the page you were on', async ({ page }) => {
    const lm = new LayoutPage(page);
    await lm.plant(layoutOf(['short'], ['tall'], []));
    await lm.goto();
    await lm.selectPage(1);
    expect(await lm.order()).toEqual(['tall-2']);

    await lm.reload();
    expect(await lm.currentLayout()).toBe(1);
    expect(await lm.order()).toEqual(['tall-2']);
  });

  test('S11: a page key typed into a field stays in the field', async ({ page }) => {
    const lm = new LayoutPage(page);
    await lm.plant(layoutOf(['controls'], ['tall'], []));
    await lm.goto();
    const input = lm.grid.locator('[data-e2e-input]');
    await input.click();
    await page.keyboard.type('note 2');
    await lm.settle();

    expect(lm.requests.filter(r => r.includes('/lm/select'))).toEqual([]);
    expect(await lm.currentLayout()).toBe(0);
    await expect(input).toBeFocused();
    await expect(input).toHaveValue('note 2');
  });

  test('S11: Ctrl/Cmd+1..9 is left to the browser, which uses it to switch tabs', async ({ page }) => {
    const lm = new LayoutPage(page);
    await lm.plant(layoutOf(['short'], ['tall'], []));
    await lm.goto();
    await page.evaluate(() => {
      window.addEventListener('keydown', e => { (window as any).__prevented = e.defaultPrevented; });
    });
    await page.keyboard.press('Control+2');
    await lm.settle();

    expect(await page.evaluate(() => (window as any).__prevented)).toBe(false);
  });
});

/** Without LayoutCount, users add, name and remove their own pages. */
test.describe('pages users add', () => {
  const addPage = async (lm: LayoutPage) => {
    await Promise.all([
      lm.page.waitForResponse(r => r.url().includes('/lm/pages/add')),
      lm.page.getByRole('button', { name: 'Add page', exact: true }).click(),
    ]);
    await lm.waitForIdle();
  };

  test('+ adds a page, a double-click names it, and Done keeps both', async ({ page }) => {
    const lm = new LayoutPage(page, 'pages');
    await lm.goto();
    await expect(page.locator('#lm-selectors'), 'one page: no strip in view mode').toBeHidden();
    await lm.edit();
    await expect(lm.pageButton(0)).toHaveText('1');
    await expect(page.getByRole('button', { name: 'Remove page', exact: true }), 'the only page stays').toBeHidden();
    const first = await lm.addBlock('short');

    await addPage(lm);
    expect(await lm.currentLayout()).toBe(1);
    await expect(lm.pageButton(1)).toHaveText('2');
    await expect(lm.pageButton(1)).toHaveClass(/ir-btn-primary/);
    await expect(page).toHaveURL(/[?&]layout=1\b/);
    expect(await lm.order()).toEqual([]);

    await lm.pageButton(1).dblclick();
    const field = page.getByRole('textbox', { name: 'Page name' });
    await expect(field).toBeFocused();
    await field.fill('Cameras');
    await Promise.all([page.waitForResponse(r => r.url().includes('/lm/pages/rename')), field.press('Enter')]);
    await lm.waitForIdle();
    await expect(lm.pageButton(1)).toHaveText('Cameras');
    const camera = await lm.addBlock('tall');

    await lm.done();
    await lm.reload();
    await expect(lm.pageButton(0)).toHaveText('1');
    await expect(lm.pageButton(1)).toHaveText('Cameras');
    expect(await lm.currentLayout()).toBe(1);
    expect(await lm.order()).toEqual([camera]);
    expect(await lm.committedOrder(0)).toEqual([first]);
  });

  test('the rename button names the page shown; Escape keeps the old name', async ({ page }) => {
    const lm = new LayoutPage(page, 'pages');
    await lm.goto();
    await lm.edit();
    const rename = page.getByRole('button', { name: 'Rename page', exact: true });
    const field = page.getByRole('textbox', { name: 'Page name' });

    await rename.click();
    await field.fill('Overview');
    await Promise.all([page.waitForResponse(r => r.url().includes('/lm/pages/rename')), field.press('Enter')]);
    await lm.waitForIdle();
    await expect(lm.pageButton(0)).toHaveText('Overview');

    await rename.click();
    await field.fill('Something else');
    await field.press('Escape');
    await lm.settle();
    await expect(lm.pageButton(0)).toHaveText('Overview');
    expect(lm.requests.filter(r => r.includes('/lm/pages/rename'))).toHaveLength(1);
  });

  test('Remove page asks before taking its blocks; Cancel brings pages back', async ({ page }) => {
    const lm = new LayoutPage(page, 'pages');
    await lm.goto();
    await lm.edit();
    await lm.addBlock('short');
    await addPage(lm);
    await lm.addBlock('tall');
    await lm.done();

    await lm.edit();
    await lm.selectPage(1);
    const remove = page.getByRole('button', { name: 'Remove page', exact: true });
    page.once('dialog', dialog => dialog.dismiss());
    await remove.click();
    await lm.settle();
    expect(lm.requests.filter(r => r.includes('/lm/pages/remove')), 'dismissed: nothing sent').toEqual([]);

    page.once('dialog', dialog => dialog.accept());
    await Promise.all([page.waitForResponse(r => r.url().includes('/lm/pages/remove')), remove.click()]);
    await lm.waitForIdle();
    expect(await lm.currentLayout()).toBe(0);
    await expect(lm.pageButton(1)).toHaveCount(0);

    await addPage(lm);
    await addPage(lm);
    expect(await lm.currentLayout()).toBe(2);
    await lm.cancel();
    expect(await lm.currentLayout(), 'the last committed page, for one Cancel dropped').toBe(1);
    await expect(lm.pageButton(1)).toBeVisible();
    await expect(lm.pageButton(2)).toHaveCount(0);
    expect(await lm.order()).toHaveLength(1);
  });

  test('a fixed number of pages has no add or remove page buttons', async ({ page }) => {
    const lm = new LayoutPage(page);
    await lm.goto();
    await lm.edit();
    await expect(page.getByRole('button', { name: 'Rename page', exact: true })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Add page', exact: true })).toHaveCount(0);
    await expect(page.getByRole('button', { name: 'Remove page', exact: true })).toHaveCount(0);
  });
});
