import { test, expect, chromium, webkit } from './fixtures';
import { LayoutPage, layoutOf } from './support/layout';

async function hooks(page: import('@playwright/test').Page) {
  return (await page.request.get('/e2e/hooks')).json() as Promise<{ db: any; saves: number; loads: number }>;
}

/** What survives a reload, what Done commits, and what other browsers see. */
test.describe('persistence', () => {
  test('a draft survives a reload; Done commits it', async ({ page }) => {
    const lm = new LayoutPage(page);
    await lm.goto();
    await lm.edit();
    const a = await lm.addBlock('short');
    await lm.reload();
    expect(await lm.order()).toEqual([a]);
    expect(await lm.committedOrder(), 'not committed yet').toEqual([]);

    await lm.edit();
    await lm.done();
    expect(await lm.committedOrder()).toEqual([a]);
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

    test.fail(true, 'S8: view mode renders the session draft, so Done/Reset are not what a viewer sees');
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

    test.fail(true, 'S9: the LoadHook only seeds a session with no draft; B never reads the store again');
    await b.reload();
    expect(await b.order(), 'B after a reload').toEqual([x, y]);
    await b.edit();
    await b.reset();
    expect(await b.order(), 'B after Reset').toEqual([x, y]);
  });

  // S17: the layout cookies are Secure (gorilla's defaults) whatever the
  // request's scheme, and a browser keeps a Secure cookie only from an origin
  // it trusts. Chromium trusts loopback; WebKit does not even trust that.
  for (const [engine, host] of [['chromium', 'dashboard.lan'], ['webkit', '127.0.0.1']] as const) {
    test(`S17: over plain HTTP at ${host} (${engine}), an edit survives a reload`, async ({ baseURL, browserName }) => {
      test.skip(browserName !== 'chromium', 'launches its own browser once, from the Chromium project');
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

        await page.reload();
        await lm.ready();
        test.fail(true, 'S17: the browser refuses the Secure layout cookies over plain HTTP, so nothing persists');
        expect(await lm.order()).toHaveLength(1);
      } finally {
        await browser.close();
      }
    });
  }
});
