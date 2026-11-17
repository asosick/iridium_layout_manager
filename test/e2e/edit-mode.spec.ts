import { test, expect } from './fixtures';
import { LayoutPage, layoutOf } from './support/layout';

/** Edit, Done and Reset, in the browser and in what the server stores. */
test.describe('edit mode', () => {
  test('Done commits and locks; Reset restores the last commit; a reload shows the commit', async ({ page }) => {
    const lm = new LayoutPage(page);
    await lm.goto();
    await expect(lm.addButton()).toBeHidden();
    await expect(lm.doneButton()).toBeHidden();

    await lm.edit();
    await expect(lm.editButton()).toBeHidden();
    const a = await lm.addBlock('short');
    await lm.done();
    expect(await lm.isEditing()).toBe(false);
    await expect(lm.editButton()).toBeVisible();
    expect(await lm.committedOrder()).toEqual([a]);

    await lm.edit();
    const b = await lm.addBlock('tall');
    await lm.remove(a);
    expect(await lm.order()).toEqual([b]);
    expect(await lm.serverOrder()).toEqual([b]);
    expect(await lm.committedOrder(), 'edits before Done are only a draft').toEqual([a]);

    await lm.reset();
    await expect(page.getByText('Layout reset')).toBeVisible();
    expect(await lm.order()).toEqual([a]);
    expect(await lm.serverOrder()).toEqual([a]);
    expect(await lm.isEditing(), 'Reset keeps editing').toBe(true);

    await lm.reload();
    expect(await lm.order()).toEqual([a]);
    expect(await lm.isEditing()).toBe(false);
  });

  test('Cancel discards the draft and returns to the committed layout in view mode', async ({ page }) => {
    const lm = new LayoutPage(page);
    await lm.plant(layoutOf(['short']));
    await lm.goto();
    const [a] = await lm.order();

    await lm.edit();
    await lm.addBlock('tall');
    await lm.cancel();
    expect(await lm.isEditing()).toBe(false);
    expect(await lm.order()).toEqual([a]);
    expect(await lm.serverOrder(), 'the draft is gone').toEqual([a]);

    await lm.edit();
    expect(await lm.order()).toEqual([a]);
  });

  test('without a lock button the page is always editable, also after Done', async ({ page }) => {
    const lm = new LayoutPage(page, 'nolock');
    await lm.goto();
    expect(await lm.isEditing()).toBe(true);
    await expect(lm.editButton()).toHaveCount(0);

    const id = await lm.addBlock('short');
    await lm.done();
    await expect(page.getByText('Layout saved')).toBeVisible();
    expect(await lm.isEditing()).toBe(true);
    await expect(lm.control(id, 'Remove block')).toBeVisible();
    expect(await lm.committedOrder()).toEqual([id]);
  });

  test.describe('when the save hook fails', () => {
    test.use({ allowedPageProblems: [/status of 500/] });

    test('Done reports the error, stays in edit mode and keeps the draft', async ({ page }) => {
      const lm = new LayoutPage(page, 'hooks');
      await lm.goto();
      await lm.edit();
      const id = await lm.addBlock('short');
      const failed = await page.request.post('/e2e/hooks', { data: { failSave: true } });
      expect(failed.status()).toBe(204);

      await lm.done();
      await expect(page.getByText('An unexpected error occurred')).toBeVisible();
      await expect(page.getByText('Layout saved')).toHaveCount(0);
      expect(await lm.isEditing()).toBe(true);
      await expect(lm.control(id, 'Remove block')).toBeVisible();
      expect(await lm.serverOrder()).toEqual([id]);
      expect(await lm.committedOrder()).toEqual([]);
      const db = await (await page.request.get('/e2e/hooks')).json();
      expect(db.db).toBeNull();
    });
  });
});
