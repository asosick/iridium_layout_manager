/**
 * Page object for a layout-manager page of the E2E app (internal/e2eapp).
 *
 * Reading order:
 *   order()        the blocks' DOM order, which is what the plugin sends to
 *                  the server after a drag;
 *   visualOrder()  the order a user reads: by top edge, then left edge;
 *   serverOrder()  the order of the draft the server keeps (GET /e2e/state).
 *
 * Every action waits for the requests it starts, then for the page to settle
 * (no /lm/ request in flight, masonry laid out).
 */
import { expect, type Locator, type Page, type Request } from '@playwright/test';

export const PANEL = '/admin';

export type BlockKind = 'short' | 'tall' | 'grow' | 'slow' | 'controls' | 'table' | 'chart' | 'stats' | 'note' | string;

export interface BlockRect {
  id: string;
  x: number;
  y: number;
  width: number;
  height: number;
}

export interface StoredBlock { id: string; type: string; cols: number }
export interface StoredState { layouts: { blocks: StoredBlock[] | null }[] | null }

const isLayoutRequest = (request: Request) => new URL(request.url()).pathname.includes('/lm/');

export class LayoutPage {
  readonly root: Locator;
  readonly grid: Locator;
  readonly blocks: Locator;
  /** Every /lm/ request the page has sent, in order: "METHOD /path?query". */
  readonly requests: string[] = [];
  private inflight = new Set<Request>();

  constructor(readonly page: Page, readonly slug = 'default') {
    this.root = page.locator('[data-lm-page]');
    this.grid = page.locator('[data-lm-grid]');
    this.blocks = page.locator('[data-lm-grid] > [data-lm-block]');
    page.on('request', request => {
      if (!isLayoutRequest(request)) return;
      const url = new URL(request.url());
      this.requests.push(`${request.method()} ${url.pathname}${url.search}`);
      this.inflight.add(request);
    });
    const done = (request: Request) => { this.inflight.delete(request); };
    page.on('requestfinished', done);
    page.on('requestfailed', done);
  }

  get path() { return `${PANEL}/${this.slug}`; }

  /** Loads the page and waits until Alpine, htmx and masonry are ready. */
  async goto() {
    await this.page.goto(this.path);
    await this.ready();
  }

  async reload() {
    await this.page.reload();
    await this.ready();
  }

  async ready() {
    await this.page.waitForFunction(() => {
      const root = document.querySelector('[data-lm-page]') as any;
      return !!root?._x_dataStack && !!(window as any).htmx && !!(window as any).Alpine;
    });
    await this.waitForIdle();
  }

  /** Waits for in-flight /lm/ requests, their swaps and the masonry pass. */
  async waitForIdle() {
    await expect.poll(() => this.inflight.size, { message: 'layout requests still in flight' }).toBe(0);
    await this.frames(2);
    await expect.poll(() => this.inflight.size).toBe(0);
    await this.frames(2);
  }

  async frames(n: number) {
    for (let i = 0; i < n; i++) {
      await this.page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => resolve(null))));
    }
  }

  /** Lets an action that is NOT expected to send anything prove it didn't. */
  async settle(ms = 500) {
    await this.page.waitForTimeout(ms);
    await this.waitForIdle();
  }

  // --- modes ------------------------------------------------------------------

  editButton() { return this.page.getByRole('button', { name: 'Edit', exact: true }); }
  doneButton() { return this.page.locator('[data-lm-done]'); }
  resetButton() { return this.page.getByRole('button', { name: 'Reset', exact: true }); }
  cancelButton() { return this.page.getByRole('button', { name: 'Cancel', exact: true }); }
  /** "Add block": opens and closes the block gallery. */
  addButton() { return this.page.locator('[data-lm-gallery-toggle]'); }

  async isEditing() {
    return (await this.root.getAttribute('class'))?.split(/\s+/).includes('lm-editing') ?? false;
  }

  async edit() {
    await this.editButton().click();
    await expect(this.root).toHaveClass(/\blm-editing\b/);
    await this.waitForIdle();
  }

  async done() {
    await Promise.all([
      this.page.waitForResponse(r => r.url().includes('/lm/save')),
      this.doneButton().click(),
    ]);
    await this.waitForIdle();
  }

  async reset() {
    await Promise.all([
      this.page.waitForResponse(r => r.url().includes('/lm/reset')),
      this.resetButton().click(),
    ]);
    await this.waitForIdle();
  }

  async cancel() {
    await Promise.all([
      this.page.waitForResponse(r => r.url().includes('/lm/reset')),
      this.cancelButton().click(),
    ]);
    await this.waitForIdle();
  }

  // --- blocks -----------------------------------------------------------------

  block(id: string) { return this.page.locator(`[data-lm-grid] > [data-lm-block][data-id="${id}"]`); }

  control(id: string, label: string) { return this.block(id).getByRole('button', { name: label, exact: true }); }

  /**
   * Adds a block of the given kind by clicking its card in the block gallery,
   * which adds it after the last; returns its ID. The gallery is left as it
   * was found: a closed one is opened for the add and closed again, so it
   * covers no block afterwards.
   */
  async addBlock(kind: BlockKind): Promise<string> {
    return (await this.addBlocks(kind))[0];
  }

  /** addBlock for each kind in turn, opening the gallery once. */
  async addBlocks(...kinds: BlockKind[]): Promise<string[]> {
    const wasOpen = await this.isGalleryOpen();
    await this.openGallery();
    if ((await this.searchField().inputValue()) !== '') await this.searchField().fill('');
    const ids: string[] = [];
    for (const kind of kinds) {
      const before = await this.order();
      await Promise.all([
        this.page.waitForResponse(r => r.url().includes('/lm/add')),
        this.cardButton(kind).click(),
      ]);
      await this.waitForIdle();
      await this.sheetSettled();
      const added = (await this.order()).filter(id => !before.includes(id));
      expect(added, `one new ${kind} block`).toHaveLength(1);
      ids.push(added[0]);
    }
    if (!wasOpen) await this.closeGallery();
    return ids;
  }

  /** Clicks a per-block control and waits for its request to finish. */
  async clickControl(id: string, label: string, path: string) {
    await Promise.all([
      this.page.waitForResponse(r => r.url().includes(path)),
      this.control(id, label).click(),
    ]);
    await this.waitForIdle();
  }

  async remove(id: string) { await this.clickControl(id, 'Remove block', '/lm/remove'); }
  async moveLeft(id: string) { await this.clickControl(id, 'Move block left', '/lm/move'); }
  async moveRight(id: string) { await this.clickControl(id, 'Move block right', '/lm/move'); }
  async grow(id: string) { await this.clickControl(id, 'Increase size', '/lm/resize'); }
  async shrink(id: string) { await this.clickControl(id, 'Decrease size', '/lm/resize'); }
  async toggleFull(id: string) { await this.clickControl(id, 'Toggle full width', '/lm/resize'); }

  async cols(id: string) { return Number(await this.block(id).getAttribute('data-cols')); }

  // --- the block gallery --------------------------------------------------------

  gallery() { return this.page.locator('[data-lm-gallery]'); }
  searchField() { return this.page.locator('[data-lm-gallery-search]'); }
  card(kind: BlockKind) { return this.page.locator(`[data-lm-card][data-key="${kind}"]`); }
  cardButton(kind: BlockKind) { return this.card(kind).locator('.lm-card-hit'); }
  slot() { return this.page.locator('[data-lm-grid] > .lm-drop-slot'); }

  async isGalleryOpen() {
    return (await this.gallery().getAttribute('class'))?.split(/\s+/).includes('is-open') ?? false;
  }

  /** Opens the gallery with "Add block" and waits for the sheet to settle. */
  async openGallery() {
    if (!(await this.isGalleryOpen())) await this.addButton().click();
    await expect(this.gallery()).toHaveClass(/\bis-open\b/);
    await this.sheetSettled();
  }

  /** Closes the gallery with "Add block" and waits for the page to settle. */
  async closeGallery() {
    if (await this.isGalleryOpen()) await this.addButton().click();
    await expect(this.gallery()).not.toHaveClass(/\bis-open\b/);
    await this.sheetSettled();
  }

  /**
   * Waits until nothing is moving: the sheet sliding, its cards rising in or
   * changing width, the room below the grid growing or shrinking with it,
   * blocks sliding aside, the page scrolling (a block a click added is
   * scrolled into view).
   */
  async sheetSettled() {
    await this.page.waitForFunction(() => {
      const sheet = document.querySelector('[data-lm-gallery]');
      const wrap = document.querySelector('.lm-grid-wrap');
      // Running ones only: a finished animation that holds its last frame is
      // still listed.
      const sliding = (el: Element | null) => !!el && el.getAnimations().some(a => a.playState === 'running');
      const cards = [...document.querySelectorAll('[data-lm-card]')].some(sliding);
      const moving = [...document.querySelectorAll('[data-lm-grid] > [data-lm-block]')]
        .some(el => el.getAnimations().some(a => (a as any).id === 'lm-flip'));
      return !moving && !cards && !sliding(sheet) && !sliding(wrap);
    });
    let last = -1;
    for (let still = 0; still < 3;) {
      const top = await this.page.evaluate(() => (document.querySelector('[data-main]') ?? document.scrollingElement!).scrollTop);
      still = top === last ? still + 1 : 0;
      last = top;
      await this.frames(1);
    }
  }

  /**
   * Drags the card of `kind` out of the gallery and releases it at `to`: a
   * point, or the left or right edge region of a block (`before`/`after` its
   * ID). It lifts the card, travels in small steps, then holds still until
   * the slot has settled, and lets go where it is. Returns the release point.
   */
  async dragCard(kind: BlockKind, to: { x: number; y: number } | { before: string } | { after: string }) {
    await this.openGallery();
    const grip = await this.cardButton(kind).boundingBox();
    expect(grip, `the ${kind} card is visible`).not.toBeNull();
    const sx = grip!.x + grip!.width / 2;
    const sy = grip!.y + grip!.height / 2;
    const mouse = this.page.mouse;
    await mouse.move(sx, sy);
    await mouse.down();
    await mouse.move(sx, sy - 12, { steps: 3 });
    await this.sheetSettled();
    let end: { x: number; y: number };
    if ('x' in to) {
      end = to;
    } else {
      const id = 'before' in to ? to.before : to.after;
      const box = await this.block(id).boundingBox();
      expect(box, `drop target ${id} is visible`).not.toBeNull();
      end = { x: box!.x + box!.width * ('before' in to ? 0.2 : 0.8), y: box!.y + box!.height / 2 };
    }
    const steps = 16;
    for (let i = 1; i <= steps; i++) {
      await mouse.move(sx + ((end.x - sx) * i) / steps, sy - 12 + ((end.y - sy + 12) * i) / steps);
      await this.page.waitForTimeout(16);
    }
    await this.frames(2);
    await this.sheetSettled();
    await mouse.up();
    return end;
  }

  /** dragCard, then waits for the block the drop adds; returns its ID. */
  async dropCard(kind: BlockKind, to: Parameters<LayoutPage['dragCard']>[1]): Promise<string> {
    const before = await this.order();
    const added = this.page.waitForResponse(r => r.url().includes('/lm/add'));
    await this.dragCard(kind, to);
    await added;
    await this.waitForIdle();
    const ids = (await this.order()).filter(id => !before.includes(id));
    expect(ids, `one new ${kind} block`).toHaveLength(1);
    return ids[0];
  }

  // --- reading order ----------------------------------------------------------

  async order(): Promise<string[]> {
    return this.blocks.evaluateAll(els => els.map(el => (el as HTMLElement).dataset.id!));
  }

  async rects(): Promise<BlockRect[]> {
    return this.blocks.evaluateAll(els => els.map(el => {
      const r = el.getBoundingClientRect();
      return { id: (el as HTMLElement).dataset.id!, x: r.left, y: r.top, width: r.width, height: r.height };
    }));
  }

  /** Reading order: top edge first, then left edge (2px tolerance). */
  async visualOrder(): Promise<string[]> {
    const rects = await this.rects();
    rects.sort((a, b) => (Math.abs(a.y - b.y) > 2 ? a.y - b.y : a.x - b.x));
    return rects.map(r => r.id);
  }

  /** Index of the grid column each block starts in (0-based). */
  async startColumns(): Promise<Record<string, number>> {
    return this.grid.evaluate(grid => {
      const styles = getComputedStyle(grid);
      const columns = styles.gridTemplateColumns.split(' ').length;
      const gap = parseFloat(styles.columnGap) || 0;
      const unit = (grid.clientWidth + gap) / columns;
      const left = grid.getBoundingClientRect().left;
      const out: Record<string, number> = {};
      grid.querySelectorAll(':scope > [data-lm-block]').forEach(el => {
        out[(el as HTMLElement).dataset.id!] = Math.round((el.getBoundingClientRect().left - left) / unit);
      });
      return out;
    });
  }

  async columnCount(): Promise<number> {
    return this.grid.evaluate(grid => getComputedStyle(grid).gridTemplateColumns.split(' ').length);
  }

  // --- server state -----------------------------------------------------------

  /**
   * The draft and committed layouts the server keeps for this browser's
   * user, read with the browser's own cookies (which say who the user is).
   */
  async storedState(): Promise<{ draft: StoredState | null; committed: StoredState | null }> {
    await this.onOrigin();
    return this.page.evaluate(async slug => {
      const response = await fetch(`/e2e/state?page=${slug}`, { credentials: 'same-origin' });
      if (!response.ok) throw new Error(`GET /e2e/state: ${response.status}`);
      return response.json();
    }, this.slug);
  }

  /** Makes sure the tab is on the app's origin, so in-page fetch can run. */
  private async onOrigin() {
    if (!this.page.url().startsWith('http')) await this.page.goto('/e2e/blank');
  }

  /** Block IDs of one layout in the stored draft (what a reload shows). */
  async serverOrder(layout = 0): Promise<string[]> {
    const { draft } = await this.storedState();
    return (draft?.layouts?.[layout]?.blocks ?? []).map(b => b.id);
  }

  async committedOrder(layout = 0): Promise<string[]> {
    const { committed } = await this.storedState();
    return (committed?.layouts?.[layout]?.blocks ?? []).map(b => b.id);
  }

  /**
   * Plants layouts for this browser's user: plant(saved) a committed layout
   * with no unsaved edits, plant(saved, draft) also unsaved edits on top.
   */
  async plant(saved: StoredState | object, draft?: StoredState | object) {
    const body: Record<string, unknown> = { committed: saved };
    if (draft !== undefined) body.draft = draft;
    await this.onOrigin();
    const status = await this.page.evaluate(async ({ slug, body }) => {
      const response = await fetch(`/e2e/state?page=${slug}`, {
        method: 'POST',
        credentials: 'same-origin',
        body: JSON.stringify(body),
      });
      return response.status;
    }, { slug: this.slug, body });
    expect(status, 'POST /e2e/state').toBe(204);
  }

  // --- pointer interactions ---------------------------------------------------

  /**
   * Drags block `id` by its handle (or `from`) and releases it over `to` (a
   * point, or the centre of another block), moving like a person: small
   * steps, then a pause over the target so the sortable engine finishes its
   * animation and settles on the final position. Returns the release point.
   */
  async drag(id: string, to: { x: number; y: number } | string, options: { from?: Locator; steps?: number } = {}) {
    const grip = options.from ?? this.block(id).locator('.lm-block-handle');
    const start = await grip.boundingBox();
    expect(start, 'drag start point is visible').not.toBeNull();
    const sx = start!.x + start!.width / 2;
    const sy = start!.y + start!.height / 2;
    // Onto a block: a quarter of its size past its centre, in the direction of
    // travel, which unambiguously takes its place (at the exact centre the
    // engines' timing decides between before and after).
    const target = async () => {
      if (typeof to !== 'string') return to;
      const box = await this.block(to).boundingBox();
      expect(box, `drop target ${to} is visible`).not.toBeNull();
      const cx = box!.x + box!.width / 2;
      const cy = box!.y + box!.height / 2;
      if (Math.abs(cy - sy) >= Math.abs(cx - sx)) return { x: cx, y: cy + Math.sign(cy - sy) * box!.height / 4 };
      return { x: cx + Math.sign(cx - sx) * box!.width / 4, y: cy };
    };
    const end = await target();
    const mouse = this.page.mouse;
    await mouse.move(sx, sy);
    await mouse.down();
    await mouse.move(sx + 4, sy + 4, { steps: 2 });
    await this.page.waitForTimeout(50);
    const steps = options.steps ?? 20;
    for (let i = 1; i <= steps; i++) {
      await mouse.move(sx + ((end.x - sx) * i) / steps, sy + ((end.y - sy) * i) / steps);
      await this.page.waitForTimeout(15);
    }
    // Hold over the target, nudging once each sort animation has finished
    // (SortableJS ignores a target while it animates) so the engine settles
    // on the final position.
    for (let i = 0; i < 4; i++) {
      await this.sortIdle();
      await mouse.move(end.x + (i % 2 ? -2 : 2), end.y + (i % 2 ? -2 : 2));
      await this.page.waitForTimeout(40);
    }
    await this.sortIdle();
    await mouse.move(end.x, end.y);
    await this.page.waitForTimeout(40);
    await mouse.up();
    return end;
  }

  /** Waits until no sortable item is mid-animation. */
  private async sortIdle() {
    await this.page.waitForFunction(
      () => ![...document.querySelectorAll('[x-sort] > *')].some(el => (el as any).animated),
      undefined,
      { timeout: 5_000 },
    );
  }

  /** Drags block `id` onto another block and waits for the reorder to land. */
  async dragOnto(id: string, targetId: string) {
    const reorder = this.page.waitForResponse(r => r.url().includes('/lm/reorder'));
    const point = await this.drag(id, targetId);
    await reorder;
    await this.waitForIdle();
    return point;
  }

  /**
   * Drags the resize handle of block `id` sideways by `deltaCols` column
   * widths (a fractional value stops between columns).
   */
  async dragResize(id: string, deltaCols: number, options: { cancel?: boolean } = {}) {
    const handle = this.block(id).locator('[data-lm-resize-handle]');
    const box = await handle.boundingBox();
    expect(box, 'resize handle is visible').not.toBeNull();
    const unit = await this.grid.evaluate(grid => {
      const styles = getComputedStyle(grid);
      const columns = styles.gridTemplateColumns.split(' ').length;
      const gap = parseFloat(styles.columnGap) || 0;
      return (grid.clientWidth + gap) / columns;
    });
    const sx = box!.x + box!.width / 2;
    const sy = box!.y + box!.height / 2;
    await this.page.mouse.move(sx, sy);
    await this.page.mouse.down();
    await this.page.mouse.move(sx + deltaCols * unit, sy, { steps: 10 });
    if (options.cancel) {
      await handle.dispatchEvent('pointercancel');
      await this.page.mouse.up();
      return;
    }
    await this.page.mouse.up();
  }

  /** Resizes block `id` to `cols` columns with the drag handle. */
  async resizeTo(id: string, cols: number) {
    const current = await this.cols(id);
    if (current === cols) return;
    const response = this.page.waitForResponse(r => r.url().includes('/lm/resize'));
    await this.dragResize(id, cols - current);
    await response;
    await this.waitForIdle();
  }

  // --- pages (layouts) --------------------------------------------------------

  pageButton(index: number) { return this.page.locator(`[data-lm-layout="${index}"]`); }

  async selectPage(index: number) {
    await Promise.all([
      this.page.waitForResponse(r => r.url().includes('/lm/select')),
      this.pageButton(index).click(),
    ]);
    await this.waitForIdle();
  }

  async currentLayout(): Promise<number> {
    return this.root.evaluate(root => (window as any).Alpine.$data(root).currentLayout);
  }
}

/** Opens a layout page in edit mode. */
export async function openEditor(page: Page, slug = 'default'): Promise<LayoutPage> {
  const lm = new LayoutPage(page, slug);
  await lm.goto();
  if (!(await lm.isEditing())) await lm.edit();
  return lm;
}

/** Fixture render counts since the last /e2e/reset. */
export async function renderCounts(page: Page): Promise<Record<string, number>> {
  const response = await page.request.get('/e2e/renders');
  return response.json();
}

/** A planted layout: blocks given as [type, cols] or type, with stable IDs. */
export function layoutOf(...layouts: (string | [string, number])[][]): StoredState {
  let n = 0;
  return {
    layouts: layouts.map(blocks => ({
      blocks: blocks.map(b => {
        const [type, cols] = typeof b === 'string' ? [b, 1] : b;
        n++;
        return { id: `${type}-${n}`, type, cols };
      }),
    })),
  };
}
