import { test, expect } from './fixtures';
import { LayoutPage, openEditor } from './support/layout';

/** The block gallery: the sheet "Add block" opens, its cards and their previews. */
test.describe('block gallery', () => {
  test('opens from the toolbar with a live, scaled-down preview of each block, and closes', async ({ page }) => {
    const lm = await openEditor(page);
    await expect(lm.gallery()).toBeHidden();
    await lm.openGallery();
    await expect(lm.gallery()).toBeVisible();
    await expect(lm.addButton()).toHaveAttribute('aria-expanded', 'true');
    await expect(page.locator('[data-lm-card]')).toHaveCount(8);
    await expect(lm.searchField(), 'a mouse goes straight to the search').toBeFocused();

    // The card shows the block itself, rendered as a preview of its own.
    const card = lm.card('short');
    await expect(card).toHaveAttribute('data-lm-preview', 'ready');
    const fixture = card.locator('[data-e2e-kind="short"]');
    await expect(fixture).toHaveAttribute('data-e2e-preview', 'true');
    await expect(fixture).toHaveAttribute('data-e2e-id', /^lm-preview-/);
    // Laid out as wide as a placed block, drawn far smaller.
    const stage = await card.locator('[data-lm-card-stage]').evaluate(el => ({
      laidOut: (el as HTMLElement).offsetWidth,
      drawn: el.getBoundingClientRect().width,
    }));
    expect(stage.drawn).toBeLessThan(stage.laidOut * 0.7);
    // Real widgets preview as on the page.
    await expect(lm.card('table').locator('tbody tr')).toHaveCount(3);
    await expect(lm.card('chart').locator('.apexcharts-canvas')).toBeVisible();
    // Nothing in a preview takes focus or clicks.
    await expect(card.locator('[data-lm-card-preview]')).toHaveAttribute('inert', '');

    await page.keyboard.press('Escape');
    await expect(lm.gallery()).toBeHidden();
    await expect(lm.addButton(), 'focus goes back to the toggle').toBeFocused();
    expect(await lm.order(), 'opening and closing adds nothing').toEqual([]);
  });

  test('the search filters the cards by name, Enter adds the first, Escape clears then closes', async ({ page }) => {
    const lm = await openEditor(page);
    await lm.openGallery();
    const shown = () => page.locator('[data-lm-card]:not([hidden])');

    await lm.searchField().fill('orders');
    await expect(shown()).toHaveCount(1);
    await expect(lm.card('table')).toBeVisible();
    await expect(lm.card('table').locator('mark')).toHaveText('Orders');
    await expect(lm.gallery().locator('.lm-gallery-count')).toHaveText('1 of 8');

    // Every word typed must match, anywhere in the name.
    await lm.searchField().fill('sta sal');
    await expect(shown()).toHaveCount(1);
    await expect(lm.card('stats')).toBeVisible();

    await lm.searchField().fill('nothing like it');
    await expect(shown()).toHaveCount(0);
    await expect(lm.gallery().getByText('No blocks match')).toBeVisible();

    await lm.searchField().fill('tall');
    await Promise.all([
      page.waitForResponse(r => r.url().includes('/lm/add')),
      lm.searchField().press('Enter'),
    ]);
    await lm.waitForIdle();
    await expect(lm.blocks).toHaveCount(1);
    await expect(lm.blocks.first().locator('[data-e2e-kind="tall"]')).toBeVisible();

    await lm.searchField().press('Escape');
    await expect(lm.searchField()).toHaveValue('');
    await expect(shown()).toHaveCount(8);
    await expect(lm.gallery(), 'the first Escape only clears').toBeVisible();
    await lm.searchField().press('Escape');
    await expect(lm.gallery()).toBeHidden();
  });

  test('a click adds the block after the last, and the gallery stays open for more', async ({ page }) => {
    const lm = await openEditor(page);
    await lm.openGallery();
    const ids: string[] = [];
    for (const kind of ['short', 'tall', 'short']) {
      const before = await lm.order();
      await Promise.all([
        page.waitForResponse(r => r.url().includes('/lm/add')),
        lm.cardButton(kind).click(),
      ]);
      await lm.waitForIdle();
      ids.push((await lm.order()).find(id => !before.includes(id))!);
      await expect(lm.gallery()).toHaveClass(/\bis-open\b/);
    }
    expect(await lm.order()).toEqual(ids);
    expect(await lm.serverOrder()).toEqual(ids);
    await expect(lm.block(ids[2]).locator('[data-e2e-kind="short"]')).toHaveAttribute('data-e2e-preview', 'false');
    // A keyboard reaches the cards from the search, and adds with Enter.
    await lm.searchField().focus();
    await page.keyboard.press('ArrowDown');
    await expect(lm.cardButton('short')).toBeFocused();
    await page.keyboard.press('ArrowRight');
    await expect(lm.cardButton('tall')).toBeFocused();
    await Promise.all([
      page.waitForResponse(r => r.url().includes('/lm/add')),
      page.keyboard.press('Enter'),
    ]);
    await lm.waitForIdle();
    await expect(lm.blocks).toHaveCount(4);
    await expect(lm.blocks.last().locator('[data-e2e-kind="tall"]')).toBeVisible();
  });

  test('a card dropped on the grid lands where it was dropped', async ({ page }) => {
    const lm = await openEditor(page);
    const [a, b, c] = await lm.addBlocks('short', 'short', 'short');

    // Between a and b.
    const between = await lm.dropCard('tall', { after: a });
    expect(await lm.order()).toEqual([a, between, b, c]);
    expect(await lm.serverOrder()).toEqual([a, between, b, c]);
    await expect(lm.block(between).locator('[data-e2e-kind="tall"]')).toBeVisible();

    // Before the first.
    const first = await lm.dropCard('grow', { before: a });
    expect(await lm.order()).toEqual([first, a, between, b, c]);
    expect(await lm.serverOrder()).toEqual([first, a, between, b, c]);

    // Below every block: after the last.
    const box = await lm.grid.boundingBox();
    const last = await lm.dropCard('short', { x: box!.x + 40, y: box!.y + box!.height + 30 });
    expect((await lm.order()).at(-1)).toBe(last);
    expect((await lm.serverOrder()).at(-1)).toBe(last);
    await expect(lm.slot()).toHaveCount(0);
  });

  test('while a card is held, the grid opens a slot for it and the sheet steps aside', async ({ page }) => {
    const lm = await openEditor(page);
    const [a, b] = await lm.addBlocks('short', 'short');
    await lm.openGallery();
    const box = await lm.block(a).boundingBox();
    const grip = await lm.cardButton('tall').boundingBox();
    const mouse = page.mouse;
    await mouse.move(grip!.x + grip!.width / 2, grip!.y + grip!.height / 2);
    await mouse.down();
    await mouse.move(box!.x + box!.width * 0.8, box!.y + box!.height / 2, { steps: 12 });
    await lm.sheetSettled();

    await expect(lm.slot()).toHaveCount(1);
    await expect(lm.slot()).toContainText('Tall');
    await expect(lm.gallery()).toHaveClass(/\bis-dragging\b/);
    expect(await page.evaluate(() => {
      const grid = document.querySelector('[data-lm-grid]')!;
      return [...grid.children].filter(el => el.matches('[data-lm-block], .lm-drop-slot'))
        .map(el => (el as HTMLElement).dataset.id ?? 'slot');
    })).toEqual([a, 'slot', b]);

    // Away from the grid the slot closes; back over it, it opens again.
    await mouse.move(5, box!.y + box!.height / 2, { steps: 4 });
    await expect(lm.slot()).toHaveCount(0);
    await mouse.move(box!.x + box!.width * 0.8, box!.y + box!.height / 2, { steps: 4 });
    await expect(lm.slot()).toHaveCount(1);

    await page.keyboard.press('Escape');
    await expect(lm.slot()).toHaveCount(0);
    await expect(lm.gallery()).not.toHaveClass(/\bis-dragging\b/);
    await mouse.up();
    await lm.settle();
    expect(await lm.order(), 'Escape put nothing down').toEqual([a, b]);
    expect(await lm.serverOrder()).toEqual([a, b]);
  });

  test('a card released over the sheet, or away from the grid, adds nothing', async ({ page }) => {
    const lm = await openEditor(page);
    const a = await lm.addBlock('short');

    const sheet = await lm.gallery().boundingBox();
    await lm.dragCard('tall', { x: sheet!.x + sheet!.width / 2, y: page.viewportSize()!.height - 10 });
    await lm.settle();
    expect(await lm.order()).toEqual([a]);

    await lm.dragCard('tall', { x: 5, y: 300 });
    await lm.settle();
    expect(await lm.order()).toEqual([a]);
    expect(await lm.serverOrder()).toEqual([a]);
    await expect(lm.gallery(), 'the sheet comes back').toHaveClass(/\bis-open\b/);
    await expect(lm.gallery()).not.toHaveClass(/\bis-dragging\b/);
  });

  test('leaving edit mode puts the gallery away', async ({ page }) => {
    const lm = await openEditor(page);
    await lm.openGallery();
    await Promise.all([page.waitForResponse(r => r.url().includes('/lm/add')), lm.cardButton('short').click()]);
    await lm.waitForIdle();
    await expect(lm.gallery()).toBeVisible();
    await lm.done();
    await expect(lm.gallery()).toBeHidden();
    await lm.edit();
    await expect(lm.gallery(), 'it stays shut until asked for').toBeHidden();
  });

  test('leaving the page while previews load reports no failed request', async ({ page }) => {
    // slow takes 300ms to render, so its preview is still loading as the
    // page is left; a browser that reports cancelled fetches (WebKit) fails
    // the test through its page problems.
    for (const next of ['/admin/noreorder', '/admin/default']) {
      const lm = new LayoutPage(page, 'noresize');
      await lm.goto();
      const loading = page.waitForRequest(r => r.url().includes('/lm/preview?type=slow'));
      await lm.editButton().click();
      // Opening the gallery starts its previews, if editing has not already.
      await lm.addButton().click();
      await loading;
      await page.goto(next);
    }
  });

  test('an empty page in edit mode opens the gallery from its empty state', async ({ page }) => {
    const lm = await openEditor(page);
    await page.getByRole('button', { name: 'Add a block', exact: true }).click();
    await expect(lm.gallery()).toHaveClass(/\bis-open\b/);
  });
});
