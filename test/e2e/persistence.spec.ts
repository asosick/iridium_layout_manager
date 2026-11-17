import { test, expect, chromium, webkit } from './fixtures';
import { LayoutPage, layoutOf } from './support/layout';

async function hooks(page: import('@playwright/test').Page) {
  return (await page.request.get('/e2e/hooks')).json() as Promise<{ db: any; saves: number; loads: number }>;
}

/** What survives a reload, what Done commits, and what other browsers see. */
test.describe('persistence', () => {
  test('a draft survives a reload and returns in edit mode; Done commits it', async ({ page }) => {
    const lm = new LayoutPage(page);
    await lm.goto();
    await lm.edit();
    const a = await lm.addBlock('short');
    await lm.reload();
    expect(await lm.order(), 'view mode shows what is committed').toEqual([]);
    expect(await lm.committedOrder(), 'not committed yet').toEqual([]);

    await Promise.all([page.waitForResponse(r => r.url().includes('/lm/select')), lm.editButton().click()]);
    await lm.waitForIdle();
    expect(await lm.order(), 'edit mode brings the draft back').toEqual([a]);
    await lm.done();
    expect(await lm.committedOrder()).toEqual([a]);
    await lm.reload();
    expect(await lm.order()).toEqual([a]);
  });

  test('S8: after leaving without Done, view mode shows the committed layout, not the draft', async ({ page }) => {
    const lm = new LayoutPage(page);
    await lm.goto();
    await lm.edit();
    const a = await lm.addBlock('short');
    await lm.done();
    await lm.edit();
    await lm.addBlock('tall');
    await lm.reload();
    expect(await lm.isEditing()).toBe(false);

    expect(await lm.order()).toEqual([a]);
  });

  test('Done calls the save hook once; a new browser starts from the saved layout', async ({ page, browser, baseURL }) => {
    const lm = new LayoutPage(page, 'hooks');
    await lm.goto();
    await lm.edit();
    const a = await lm.addBlock('short');
    const b = await lm.addBlock('tall');
    expect((await hooks(page)).saves, 'edits do not call the hook').toBe(0);
    await lm.done();
    const saved = await hooks(page);
    expect(saved.saves).toBe(1);
    expect(saved.db.layouts[0].blocks.map((x: any) => x.id)).toEqual([a, b]);

    const other = await browser.newContext({ baseURL });
    const second = new LayoutPage(await other.newPage(), 'hooks');
    await second.goto();
    expect(await second.order()).toEqual([a, b]);
    await other.close();
  });

  test('S9: a browser that has edited keeps its own copy after another browser saves; Reset does not re-read', async ({ page, browser, baseURL }) => {
    // Browser B edits and saves first.
    const b = new LayoutPage(page, 'hooks');
    await b.goto();
    await b.edit();
    const x = await b.addBlock('short');
    await b.done();

    // Browser A starts from B's save, adds a block and saves.
    const context = await browser.newContext({ baseURL });
    const a = new LayoutPage(await context.newPage(), 'hooks');
    await a.goto();
    expect(await a.order()).toEqual([x]);
    await a.edit();
    const y = await a.addBlock('tall');
    await a.done();
    expect((await hooks(page)).db.layouts[0].blocks.map((block: any) => block.id)).toEqual([x, y]);
    await context.close();

    await b.reload();
    expect(await b.order(), 'B after a reload').toEqual([x, y]);
    await b.edit();
    await b.reset();
    expect(await b.order(), 'B after Reset').toEqual([x, y]);
  });

  // S17: layouts used to live in Secure cookies, which a browser keeps only
  // from an origin it trusts over HTTP: Chromium trusts loopback, WebKit not
  // even that. Layouts now live on the server.
  for (const [engine, host] of [['chromium', 'dashboard.lan'], ['webkit', '127.0.0.1']] as const) {
    test(`S17: over plain HTTP at ${host} (${engine}), a saved edit survives a reload`, async ({ baseURL, browserName }) => {
      test.skip(browserName !== 'chromium', 'launches its own browser once, from the Chromium project');
      test.slow(); // a browser of its own to launch
      const launcher = engine === 'chromium' ? chromium : webkit;
      // For Chromium, a LAN host name resolved to this server: a plain-HTTP
      // origin that, unlike 127.0.0.1, it does not treat as secure.
      const browser = await launcher.launch(engine === 'chromium' ? { args: ['--host-resolver-rules=MAP dashboard.lan 127.0.0.1'] } : {});
      try {
        const page = await browser.newPage();
        await page.goto(`http://${host}:${new URL(baseURL!).port}/admin/default`);
        const lm = new LayoutPage(page);
        await lm.ready();
        await lm.edit();
        await page.locator('[data-lm-type-select]').selectOption('short');
        await Promise.all([page.waitForResponse(r => r.url().includes('/lm/add')), lm.addButton().click()]);
        await lm.waitForIdle();
        expect(await lm.order(), 'the add rendered').toHaveLength(1);
        await lm.done();

        await page.reload();
        await lm.ready();
        expect(await lm.order()).toHaveLength(1);
      } finally {
        await browser.close();
      }
    });
  }
});
