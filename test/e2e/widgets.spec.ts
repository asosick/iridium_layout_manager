import { test, expect } from './fixtures';
import { LayoutPage, layoutOf } from './support/layout';

/** Real Iridium widgets (table, chart, stats) placed as blocks. */
const clients = (lm: LayoutPage, id: string) =>
  lm.block(id).locator('tbody tr[data-row-id] td[data-col-key="Client"]');
const sortBy = (lm: LayoutPage, id: string, column: string) =>
  lm.block(id).locator(`[data-sort-col="${column}"]`);
const tableRequest = (url: string) => url.includes('/admin/default/orders');

test.describe('widgets', () => {
  test('S14: two copies of a table sort, page and filter independently', async ({ page }) => {
    const lm = new LayoutPage(page);
    await lm.plant(layoutOf(['table', 'table']));
    await lm.goto();
    const [first, second] = ['table-1', 'table-2'];
    await expect(clients(lm, first)).toHaveText(['Alder', 'Birch', 'Cedar']);
    await expect(clients(lm, second)).toHaveText(['Alder', 'Birch', 'Cedar']);

    await sortBy(lm, first, 'Client').click();
    await sortBy(lm, first, 'Client').click();
    await expect(clients(lm, first)).toHaveText(['Gum', 'Fir', 'Elm']);
    await expect(clients(lm, second)).toHaveText(['Alder', 'Birch', 'Cedar']);

    await lm.block(second).getByRole('button', { name: 'Next' }).click();
    await expect(clients(lm, second)).toHaveText(['Dogwood', 'Elm', 'Fir']);
    await expect(clients(lm, first)).toHaveText(['Gum', 'Fir', 'Elm']);

    await lm.block(second).getByRole('button', { name: 'Filters' }).click();
    await lm.block(second).locator('[id="filter-Status-trigger"]').click();
    await page.getByRole('option', { name: 'Pending' }).click();
    await expect(clients(lm, second)).toHaveText(['Birch', 'Fir']);
    await expect(clients(lm, first)).toHaveText(['Gum', 'Fir', 'Elm']);
  });

  test('S14: two copies of a chart and of a stats widget each render in their own block', async ({ page }) => {
    const lm = new LayoutPage(page);
    await lm.plant(layoutOf(['chart', 'chart', 'stats', 'stats']));
    await lm.goto();
    for (const id of ['chart-1', 'chart-2']) {
      await expect(lm.block(id).locator('.apexcharts-canvas')).toHaveCount(1);
      await expect(lm.block(id).locator('.apexcharts-bar-area').first()).toBeVisible();
    }
    for (const id of ['stats-3', 'stats-4']) {
      await expect(lm.block(id).getByText('Revenue', { exact: true })).toBeVisible();
      await expect(lm.block(id).getByText('12840')).toBeVisible();
    }
  });

  test('S14: copies of the same widget share no DOM ids', async ({ page }) => {
    const lm = new LayoutPage(page);
    await lm.plant(layoutOf(['table', 'table', 'chart', 'chart', 'stats', 'stats']));
    await lm.goto();
    const duplicates = await lm.grid.evaluate(grid => {
      const count: Record<string, number> = {};
      grid.querySelectorAll('[id]').forEach(el => { count[el.id] = (count[el.id] ?? 0) + 1; });
      return Object.keys(count).filter(id => count[id] > 1).sort();
    });

    test.fail(true, 'S14: chart ids come from the widget slug; the table only prefixes its root, not its filter, search and pagination ids');
    expect(duplicates).toEqual([]);
  });

  test('S5: a table keeps its sort and page through an edit to another block', async ({ page }) => {
    const lm = new LayoutPage(page);
    await lm.plant(layoutOf(['table', 'short']));
    await lm.goto();
    await lm.edit();
    await sortBy(lm, 'table-1', 'Client').click();
    await sortBy(lm, 'table-1', 'Client').click();
    await expect(clients(lm, 'table-1')).toHaveText(['Gum', 'Fir', 'Elm']);
    await lm.block('table-1').getByRole('button', { name: 'Next' }).click();
    await expect(clients(lm, 'table-1')).toHaveText(['Dogwood', 'Cedar', 'Birch']);

    await lm.grow('short-2');
    await page.waitForTimeout(300);

    test.fail(true, 'S5: the edit re-renders and swaps every block; the table starts over unsorted on page 1');
    await expect(clients(lm, 'table-1')).toHaveText(['Dogwood', 'Cedar', 'Birch'], { timeout: 2_000 });
  });

  test('a table\'s modal header action and row actions work inside a block', async ({ page }) => {
    const lm = new LayoutPage(page);
    await lm.plant(layoutOf(['table']));
    await lm.goto();

    await lm.block('table-1').getByRole('button', { name: 'Add note' }).click();
    const notes = page.getByLabel('Notes');
    await expect(notes).toBeVisible();
    await page.keyboard.press('Escape');
    await expect(notes).toBeHidden();

    const [inspected] = await Promise.all([
      page.waitForResponse(r => tableRequest(r.url()) && r.url().includes('/inspect')),
      lm.block('table-1').getByRole('button', { name: 'Inspect' }).first().click(),
    ]);
    expect(inspected.status()).toBeLessThan(400);
    expect(inspected.headers()['x-e2e-inspected']).toBe('1');
  });
});
