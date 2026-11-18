import { test, expect } from './fixtures';
import { LayoutPage, layoutOf, renderCounts } from './support/layout';

/**
 * What one edit costs on a page of twelve blocks: how many fixture blocks
 * the server renders (the table's row queries counted separately) and how
 * long the request takes. `slow` takes 300ms per render, like a block with
 * a slow query. The numbers are attached to the report as a baseline.
 */
const TWELVE = ['slow', 'short', 'short', 'short', 'short', 'tall', 'grow', 'controls', 'table', 'chart', 'stats', 'short'];

test('S5: one edit renders only what it changes', async ({ page }, testInfo) => {
  test.slow();
  const lm = new LayoutPage(page);
  await lm.plant(layoutOf(TWELVE, ['short']));
  await lm.goto();
  await lm.edit();
  // Editing starts the gallery's previews loading: let them finish first,
  // so no preview render lands in an edit's count.
  await lm.openGallery();
  await lm.waitForIdle();
  await lm.closeGallery();
  await lm.waitForIdle();
  const ids = await lm.order();
  const short = ids[1];

  const measure = async (name: string, path: string, act: () => Promise<unknown>) => {
    await page.request.post('/e2e/reset');
    const started = Date.now();
    const [response] = await Promise.all([page.waitForResponse(r => r.url().includes(path)), act()]);
    const ms = Date.now() - started;
    await lm.waitForIdle();
    await page.waitForTimeout(300); // client-side table refreshes, if any
    const counts = await renderCounts(page);
    const { 'table-query': tableQueries = 0, ...blocks } = counts;
    const rendered = Object.values(blocks).reduce((sum, n) => sum + n, 0);
    return { edit: name, status: response.status(), ms, fixtureRenders: rendered, tableQueries, byBlock: counts };
  };

  const results = [];
  results.push(await measure('resize', '/lm/resize', () => lm.control(short, 'Increase size').click()));
  results.push(await measure('move', '/lm/move', () => lm.control(short, 'Move block right').click()));
  await lm.openGallery();
  await lm.waitForIdle();
  results.push(await measure('add', '/lm/add', () => lm.cardButton('short').click()));
  await lm.closeGallery();
  const added = (await lm.order()).at(-1)!;
  results.push(await measure('remove', '/lm/remove', () => lm.control(added, 'Remove block').click()));
  // A reorder through the plugin's own handler, as a drop calls it (last:
  // after a reorder htmx strips the block controls, see S18).
  results.push(await measure('reorder', '/lm/reorder', () => page.evaluate(() => {
    const grid = document.querySelector('[data-lm-grid]')!;
    const blocks = grid.querySelectorAll(':scope > [data-lm-block]');
    grid.insertBefore(blocks[2], blocks[1]);
    (window as any).Alpine.$data(document.querySelector('[data-lm-page]')).handleReorder();
  })));
  results.push(await measure('switch page', '/lm/select', () => lm.pageButton(1).click()));
  results.push(await measure('switch back', '/lm/select', () => lm.pageButton(0).click()));

  console.table(results.map(({ byBlock, ...row }) => row));
  await testInfo.attach('per-edit cost', { body: JSON.stringify(results, null, 2), contentType: 'application/json' });
  for (const r of results) expect(r.status, r.edit).toBeLessThan(300);

  for (const r of results.filter(r => !r.edit.startsWith('switch'))) {
    expect(r.fixtureRenders, `${r.edit}: fixture blocks rendered`).toBeLessThanOrEqual(1);
  }
});
