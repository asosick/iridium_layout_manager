import { test, expect } from './fixtures';
import { LayoutPage, layoutOf } from './support/layout';

test.describe('zen', () => {
  test('Zen hides the page header and Escape brings it back; pages without Zen offer no button', async ({ page }) => {
    const lm = new LayoutPage(page, 'zen');
    await lm.plant(layoutOf(['short']));
    await lm.goto();
    const toolbar = page.locator('.lm-toolbar');
    await expect(toolbar).toBeVisible();
    await expect(page.getByRole('button', { name: 'Exit Zen' })).toBeHidden();

    await page.getByRole('button', { name: 'Zen', exact: true }).click();
    await expect(toolbar).toBeHidden();
    await expect(page.getByRole('heading', { name: 'Zen' })).toBeHidden();
    await expect(lm.blocks).toHaveCount(1);

    await page.keyboard.press('Escape');
    await expect(toolbar).toBeVisible();
    await expect(page.getByRole('heading', { name: 'Zen' })).toBeVisible();

    const plain = new LayoutPage(page);
    await plain.goto();
    await expect(page.getByRole('button', { name: 'Zen', exact: true })).toHaveCount(0);
  });

  test('S19: the Exit Zen button can be seen and clicked', async ({ page }) => {
    const lm = new LayoutPage(page, 'zen');
    await lm.plant(layoutOf(['short']));
    await lm.goto();
    await page.getByRole('button', { name: 'Zen', exact: true }).click();
    const exit = page.getByRole('button', { name: 'Exit Zen' });
    await expect(exit).toBeVisible();

    const box = (await exit.boundingBox())!;
    const onTop = await page.evaluate(({ x, y }) => {
      const hit = document.elementFromPoint(x, y);
      return hit?.closest('button')?.textContent?.trim() ?? hit?.className ?? null;
    }, { x: box.x + box.width / 2, y: box.y + box.height / 2 });

    test.fail(true, 'S19: the fixed Exit Zen button sits under the panel\'s top bar, which covers it');
    expect(onTop, 'element under the middle of Exit Zen').toBe('Exit Zen');
    await exit.click({ timeout: 2_000 });
    await expect(page.locator('.lm-toolbar')).toBeVisible();
  });
});
