// Iridium Layout Manager — client glue.
//
// Two Alpine components (registered with Alpine.data, as Iridium's Alpine
// build is CSP-mode):
//
//   layout_manager        the page: modes, pages, the toolbar, the order of
//                         the blocks and the one queue every request goes
//                         through, so each edit reaches the server after the
//                         one before it has landed.
//   layout_manager_block  one block: its span, its controls, drag-to-resize
//                         and the masonry row span its content needs. It sits
//                         inside the page component, so it reaches the page's
//                         state and methods through Alpine's scope.
//
// Edits change the page in place: the server answers 204 (or, for Add, the
// one new block), and the other blocks keep their state. Only switching page,
// entering edit mode with unsaved changes, and Reset load a whole grid.
//
// Iridium loads this plugin asset before Alpine starts, which also makes the
// components available to later HTMX page fragments.

(function () {
    // Keys typed into these never switch page.
    const EDITABLE = 'input, textarea, select, [contenteditable=""], [contenteditable="true"]';

    const layoutManager = () => ({
        editMode: false,
        // Mirrors the first <option> by default; Add posts the bound value.
        selectedType: '',
        // Which page (layout) is shown: the selector highlight and every
        // request's layout.
        currentLayout: 0,
        // The pages, each one's name ('' shows its number), and which hold
        // blocks, from the grid shown (data-lm-names, data-lm-populated).
        pages: [],
        populated: [],
        // Whether the user adds and removes pages (no fixed LayoutCount), and
        // names them.
        addablePages: false,
        renamablePages: false,
        // The page being renamed (its index, else -1) and the name typed.
        renaming: -1,
        renameValue: '',
        // The block IDs in the order they show.
        order: [],
        lockAfterDone: true,
        zenMode: false,
        // The page root. Not this.$el: in a method an Alpine directive calls
        // (@click on a toolbar button), $el is that button.
        _root: null,
        _urls: null,
        _queue: null,
        _reorderQueued: false,
        _listeners: null,

        init() {
            const root = this.$el;
            this._root = root;
            const select = root.querySelector('[data-lm-type-select]');
            if (select && select.options.length > 0) this.selectedType = select.options[0].value;

            const data = root.dataset;
            this._urls = {
                select: data.lmSelectUrl || '',
                add: data.lmAddUrl || '',
                save: data.lmSaveUrl || '',
                reset: data.lmResetUrl || '',
                addPage: data.lmAddPageUrl || '',
                removePage: data.lmRemovePageUrl || '',
                renamePage: data.lmRenamePageUrl || '',
            };
            this.lockAfterDone = data.lmLockAfterDone !== 'false';
            this.editMode = data.lmEditDefault === 'true';
            this.addablePages = data.lmAddablePages === 'true';
            this.renamablePages = data.lmRenamablePages === 'true';
            this.currentLayout = parseInt(data.lmCurrentLayout || '0', 10) || 0;
            this._queue = Promise.resolve();

            // Kept so destroy() can detach them when Iridium's single-page
            // navigation takes the page away.
            this._listeners = {
                hotkey: (e) => this.onHotkey(e),
                zenEscape: (e) => this.onZenEscape(e),
                // SortableJS reports a finished drag as a 'sort' event on the
                // grid; a sortable list inside a block reports its own.
                sort: (e) => {
                    if (e.target === this.grid()) this.handleReorder();
                },
            };
            document.addEventListener('keydown', this._listeners.hotkey);
            // Capture Escape before page-level shortcuts so leaving Zen mode
            // cannot trigger another action on the same key press.
            window.addEventListener('keydown', this._listeners.zenEscape, true);
            root.addEventListener('sort', this._listeners.sort);
            this.gridShown();
        },

        destroy() {
            const l = this._listeners;
            if (!l) return;
            document.removeEventListener('keydown', l.hotkey);
            window.removeEventListener('keydown', l.zenEscape, true);
            this._root.removeEventListener('sort', l.sort);
            this._listeners = null;
        },

        // --- the grid -----------------------------------------------------------

        grid() {
            return this._root.querySelector('[data-lm-grid]');
        },

        blockElements() {
            const grid = this.grid();
            return grid ? Array.from(grid.querySelectorAll(':scope > [data-lm-block]')) : [];
        },

        // A grid was rendered: read its pages and order, then switch masonry
        // on once its blocks have measured themselves.
        gridShown() {
            const grid = this.grid();
            if (!grid) return;
            this.populated = (grid.dataset.lmPopulated || '').split(',').map(flag => flag === '1');
            let names = null;
            try { names = JSON.parse(grid.dataset.lmNames || 'null'); } catch (err) { /* numbers below */ }
            this.pages = Array.isArray(names) ? names : this.populated.map(() => '');
            // The page shown can differ from the one asked for: a page added
            // or removed, or one the committed layout does not have.
            const shown = parseInt(grid.dataset.lmShown || '', 10);
            if (Number.isInteger(shown) && shown !== this.currentLayout) this.keepInAddress(shown);
            this.syncOrder();
            window.requestAnimationFrame(() => grid.classList.add('lm-masonry-ready'));
        },

        // Blocks were added, removed or moved in the grid shown.
        syncOrder() {
            this.order = this.blockElements().map(el => el.dataset.id);
            if (this.currentLayout < this.populated.length) {
                this.populated[this.currentLayout] = this.order.length > 0;
            }
        },

        selectorsShown() {
            return this.editMode || this.populated.filter(Boolean).length > 1;
        },

        pageShown(i) {
            return this.editMode || !!this.populated[i];
        },

        pageLabel(i) {
            return this.pages[i] || this.pageNumber(i);
        },

        pageNumber(i) {
            return String(i + 1);
        },

        pageClass(i) {
            return this.currentLayout === i ? 'ir-btn-primary' : 'ir-btn-outline';
        },

        isRenaming(i) {
            return this.renaming === i;
        },

        renameFieldClass(i) {
            return this.renaming === i ? '' : 'lm-page-name-hidden';
        },

        canRemovePage() {
            return this.editMode && this.addablePages && this.pages.length > 1;
        },

        // --- requests -------------------------------------------------------------

        // enqueue runs task once every earlier task has finished.
        enqueue(task) {
            const run = this._queue.then(task).catch(err => {
                console.error('[layoutmgr]', err);
            });
            this._queue = run;
            return run;
        },

        // post sends an edit. A refused one is reported as Iridium reports a
        // failed htmx request: an error toast with the server's message.
        async post(url, body) {
            const security = window.Alpine.store('security');
            const headers = { 'HX-Request': 'true' };
            if (security && security.csrf) headers['X-CSRF-Token'] = security.csrf;
            if (body) headers['Content-Type'] = 'application/x-www-form-urlencoded';
            let response;
            try {
                response = await fetch(url, { method: 'POST', headers, credentials: 'same-origin', body });
            } catch (err) {
                this.notifyError('The server could not be reached.');
                return { ok: false, status: 0, headers: new Headers() };
            }
            const token = response.headers.get('X-CSRF-Token');
            if (token && security) security.csrf = token;
            if (!response.ok) {
                let description = '';
                try { description = (await response.text()).trim(); } catch (err) { /* no body */ }
                this.notifyError(description);
                return response;
            }
            // The events the response names in HX-Trigger (Iridium's toasts).
            const trigger = response.headers.get('HX-Trigger');
            if (trigger) {
                try {
                    const events = JSON.parse(trigger);
                    Object.keys(events).forEach(name => {
                        window.dispatchEvent(new CustomEvent(name, { detail: events[name] }));
                    });
                } catch (err) { /* not JSON: no events */ }
            }
            return response;
        },

        notifyError(description) {
            window.dispatchEvent(new CustomEvent('notify', {
                detail: { type: 'error', message: 'Request failed', description },
            }));
        },

        // swapGrid replaces the grid through htmx, which gives the new
        // widgets' htmx controls this page's nonce and initialises them.
        async swapGrid(method, url) {
            const grid = this.grid();
            if (!grid) return;
            await window.htmx.ajax(method, url, { source: grid, target: grid, swap: 'outerHTML' });
            this.gridShown();
        },

        loadPage(i) {
            return this.swapGrid('GET', this._urls.select + '?layout=' + i + '&mode=' + (this.editMode ? 'edit' : 'view'));
        },

        // --- toolbar --------------------------------------------------------------

        addBlock() {
            const layout = this.currentLayout;
            const url = this._urls.add + '?type=' + encodeURIComponent(this.selectedType) + '&layout=' + layout;
            this.enqueue(async () => {
                const grid = this.grid();
                if (!grid || layout !== this.currentLayout) return;
                await window.htmx.ajax('POST', url, { source: grid, target: grid, swap: 'beforeend' });
                this.syncOrder();
            });
        },

        save() {
            this.enqueue(async () => {
                const response = await this.post(this._urls.save, 'layout=' + this.currentLayout);
                if (!response.ok) return;
                const grid = this.grid();
                if (grid) grid.dataset.lmUnsaved = 'false';
                if (this.lockAfterDone) this.editMode = false;
                const next = parseInt(response.headers.get('X-Iridium-Layout-Manager-Next-Layout') || '', 10);
                if (Number.isInteger(next) && next >= 0 && next < this.pages.length && next !== this.currentLayout) {
                    this.selectLayout(next);
                }
            });
        },

        // reset discards the draft and keeps editing; cancel also leaves edit
        // mode, back to the committed layout.
        reset() {
            this.enqueue(() => this.swapGrid('POST', this._urls.reset + '?layout=' + this.currentLayout));
        },

        cancel() {
            this.enqueue(async () => {
                await this.swapGrid('POST', this._urls.reset + '?layout=' + this.currentLayout);
                this.editMode = false;
            });
        },

        startEditing() {
            this.editMode = true;
            // View mode shows the committed layout: load the draft if it differs.
            const grid = this.grid();
            if (grid && grid.dataset.lmUnsaved === 'true') {
                this.enqueue(() => this.loadPage(this.currentLayout));
            }
        },

        // selectLayout shows page i.
        selectLayout(i) {
            this.keepInAddress(i);
            this.enqueue(() => this.loadPage(i));
        },

        // keepInAddress makes page i the current one and keeps it in the
        // address, so a reload stays on it.
        keepInAddress(i) {
            this.currentLayout = i;
            const url = new URL(window.location.href);
            url.searchParams.set('layout', String(i));
            window.history.replaceState(window.history.state, '', url);
        },

        // 1..9 switches page, unless the key is typed into a field or comes
        // with a modifier: Ctrl/Cmd+digit belongs to the browser's tabs.
        onHotkey(e) {
            if (e.defaultPrevented || e.isComposing || e.metaKey || e.ctrlKey || e.altKey) return;
            if (this.pages.length <= 1 || !/^[1-9]$/.test(e.key)) return;
            if (e.target && e.target.closest && e.target.closest(EDITABLE)) return;
            const n = parseInt(e.key, 10);
            if (n > this.pages.length) return;
            e.preventDefault();
            this.selectLayout(n - 1);
        },

        // --- pages ------------------------------------------------------------------
        //
        // Adding, removing and renaming pages are edits like any other: they
        // change the draft, which Done commits and Reset or Cancel discards.

        // addPage adds an empty page after the last and shows it.
        addPage() {
            if (!this.addablePages) return;
            this.enqueue(() => this.swapGrid('POST', this._urls.addPage));
        },

        // removePage removes the page shown, with its blocks (asking first if
        // it has any), and shows the one before it.
        removePage() {
            if (!this.canRemovePage()) return;
            const layout = this.currentLayout;
            if (this.order.length > 0
                && !window.confirm('Remove page "' + this.pageLabel(layout) + '" and its blocks?')) return;
            this.enqueue(() => this.swapGrid('POST', this._urls.removePage + '?layout=' + layout));
        },

        // startRename shows a field in place of page i's button.
        startRename(i) {
            if (!this.editMode || !this.renamablePages || i < 0 || i >= this.pages.length) return;
            if (i !== this.currentLayout) this.selectLayout(i);
            this.renaming = i;
            this.renameValue = this.pages[i] || '';
            this.$nextTick(() => {
                const field = this._root.querySelectorAll('#lm-selectors .lm-page-name')[i];
                if (field) {
                    field.focus();
                    field.select();
                }
            });
        },

        // commitRename keeps the name typed (Enter, or leaving the field); an
        // empty name shows the page's number again.
        commitRename() {
            const i = this.renaming;
            if (i < 0) return;
            this.renaming = -1;
            const name = (this.renameValue || '').trim();
            if (name === (this.pages[i] || '')) return;
            this.enqueue(async () => {
                const body = new URLSearchParams({ name }).toString();
                const response = await this.post(this._urls.renamePage + '?layout=' + i, body);
                if (response.ok) this.pages[i] = await response.text();
            });
        },

        cancelRename() {
            this.renaming = -1;
        },

        enterZen() {
            this.zenMode = true;
        },

        exitZen() {
            this.zenMode = false;
        },

        onZenEscape(e) {
            if (!this.zenMode || e.key !== 'Escape') return;
            e.preventDefault();
            e.stopPropagation();
            this.exitZen();
        },

        // --- order ----------------------------------------------------------------

        // A finished drag: persist the order the grid now shows. A drag made
        // while a reorder is on its way is sent after it, with the order the
        // grid shows then.
        handleReorder() {
            this.syncOrder();
            if (this._reorderQueued) return;
            this._reorderQueued = true;
            this.enqueue(async () => {
                this._reorderQueued = false;
                const grid = this.grid();
                if (!grid || this.order.length === 0) return;
                const body = new URLSearchParams(this.order.map(id => ['order', id])).toString();
                const response = await this.post(grid.dataset.reorderUrl, body);
                // Refused: show the order the server kept.
                if (!response.ok) await this.loadPage(this.currentLayout);
            });
        },
    });

    const layoutManagerBlock = () => ({
        id: '',
        cols: 1,
        // Distinct names from the page component's: Alpine resolves this.x
        // through the block's scope first, then the page's.
        _el: null,
        _endpoints: null,
        _observer: null,
        _resize: null,

        init() {
            const el = this.$el;
            this._el = el;
            this.id = el.dataset.id || '';
            this.cols = parseInt(el.dataset.cols || '1', 10) || 1;
            this._endpoints = {
                remove: el.dataset.removeUrl || '',
                resize: el.dataset.resizeUrl || '',
                move: el.dataset.moveUrl || '',
            };
            // Masonry: the block spans as many small grid rows as its content
            // and controls need, measured again whenever either changes size.
            this._observer = new ResizeObserver(() => this.fitRows());
            el.querySelectorAll(':scope > .lm-block-content, :scope > .lm-block-controls')
                .forEach(part => this._observer.observe(part));
            this.fitRows();
        },

        destroy() {
            if (this._observer) this._observer.disconnect();
            this._observer = null;
            this.endResize();
        },

        atStart() {
            return this.order.indexOf(this.id) <= 0;
        },

        atEnd() {
            const i = this.order.indexOf(this.id);
            return i < 0 || i === this.order.length - 1;
        },

        // --- edits ------------------------------------------------------------------

        remove() {
            const el = this._el;
            const url = this._endpoints.remove;
            this.enqueue(async () => {
                // A second click on a block already removed sends nothing.
                if (!el.isConnected) return;
                const response = await this.post(url);
                if (!response.ok) return;
                el.remove();
                this.syncOrder();
            });
        },

        shrink() { this.resizeTo('&delta=-1'); },
        grow() { this.resizeTo('&delta=1'); },
        toggleFull() { this.resizeTo('&cols=0'); },

        // resizeTo asks for a new span and shows the one the server stored.
        resizeTo(query, fallback) {
            const el = this._el;
            const url = this._endpoints.resize + query;
            this.enqueue(async () => {
                if (!el.isConnected) return;
                const response = await this.post(url);
                const stored = parseInt(response.headers.get('X-Layout-Manager-Cols') || '', 10);
                if (response.ok && stored > 0) {
                    this.cols = stored;
                } else if (fallback) {
                    this.cols = fallback;
                }
            });
        },

        moveLeft() { this.moveBy(-1); },
        moveRight() { this.moveBy(1); },

        moveBy(delta) {
            const el = this._el;
            const url = this._endpoints.move + '&delta=' + delta;
            this.enqueue(async () => {
                if (!el.isConnected) return;
                const response = await this.post(url);
                if (!response.ok) return;
                // Move the neighbour across the block rather than the block, so
                // a focused control inside it keeps focus.
                const isBlock = node => node && node.matches && node.matches('[data-lm-block]');
                const neighbour = delta < 0 ? el.previousElementSibling : el.nextElementSibling;
                if (isBlock(neighbour)) {
                    if (delta < 0) el.after(neighbour);
                    else el.before(neighbour);
                }
                this.syncOrder();
            });
        },

        // --- drag to resize -----------------------------------------------------------

        startResize(e) {
            if (!this.editMode || (e.button !== undefined && e.button !== 0)) return;
            const el = this._el;
            const grid = el.parentElement;
            if (!grid) return;
            e.preventDefault();
            e.stopPropagation();

            const styles = window.getComputedStyle(grid);
            const columns = Math.max(1, styles.gridTemplateColumns.split(' ').length);
            const gap = parseFloat(styles.columnGap) || 0;
            const unit = (grid.clientWidth + gap) / columns;
            const startColumn = Math.max(0, Math.round((el.getBoundingClientRect().left - grid.getBoundingClientRect().left) / unit));
            this._resize = {
                grid,
                columns,
                unit,
                startColumn,
                startX: e.clientX,
                startCols: this.cols,
                move: (event) => this.previewResize(event),
                end: (event) => this.finishResize(event),
            };
            grid.classList.add('lm-resizing');
            el.classList.add('lm-block-resizing');
            this.showGuide();
            document.addEventListener('pointermove', this._resize.move);
            document.addEventListener('pointerup', this._resize.end);
            document.addEventListener('pointercancel', this._resize.end);
        },

        previewResize(e) {
            const state = this._resize;
            if (!state) return;
            e.preventDefault();
            // Up to the full grid, as the + button allows: a block too wide for
            // its row moves to the next one.
            const delta = Math.round((e.clientX - state.startX) / state.unit);
            this.cols = Math.max(1, Math.min(state.startCols + delta, state.columns));
            this.showGuide();
        },

        // The guide highlights the columns the block will cover.
        showGuide() {
            const state = this._resize;
            if (!state) return;
            const start = state.startColumn + this.cols > state.columns ? 0 : state.startColumn;
            state.grid.querySelectorAll('.lm-grid-guide-cell').forEach((cell, index) => {
                cell.classList.toggle('is-active', index >= start && index < start + this.cols);
            });
        },

        finishResize(e) {
            const state = this._resize;
            if (!state) return;
            const cols = this.cols;
            this.endResize();
            if (e.type === 'pointercancel' || cols === state.startCols) {
                this.cols = state.startCols;
                return;
            }
            this.resizeTo('&cols=' + cols, state.startCols);
        },

        endResize() {
            const state = this._resize;
            if (!state) return;
            document.removeEventListener('pointermove', state.move);
            document.removeEventListener('pointerup', state.end);
            document.removeEventListener('pointercancel', state.end);
            state.grid.classList.remove('lm-resizing');
            this._el.classList.remove('lm-block-resizing');
            state.grid.querySelectorAll('.lm-grid-guide-cell').forEach(cell => cell.classList.remove('is-active'));
            this._resize = null;
        },

        // --- masonry -------------------------------------------------------------------

        fitRows() {
            const el = this._el;
            const grid = el.parentElement;
            if (!grid) return;
            const gridStyles = window.getComputedStyle(grid);
            const rowHeight = parseFloat(gridStyles.getPropertyValue('--lm-masonry-row')) || 8;
            const rowGap = parseFloat(gridStyles.rowGap) || 16;
            const styles = window.getComputedStyle(el);
            const content = el.querySelector(':scope > .lm-block-content');
            const controls = el.querySelector(':scope > .lm-block-controls');
            const controlsHeight = controls && controls.offsetParent !== null ? controls.scrollHeight : 0;
            const contentHeight = content ? content.scrollHeight : 0;
            const innerGap = controlsHeight > 0 && contentHeight > 0 ? parseFloat(styles.rowGap) || 0 : 0;
            const chrome = (parseFloat(styles.paddingTop) || 0) + (parseFloat(styles.paddingBottom) || 0)
                + (parseFloat(styles.borderTopWidth) || 0) + (parseFloat(styles.borderBottomWidth) || 0);
            const height = Math.max(parseFloat(styles.minHeight) || 0, controlsHeight + contentHeight + innerGap + chrome);
            el.style.gridRowEnd = 'span ' + Math.max(1, Math.ceil((height + rowGap) / (rowHeight + rowGap)));
        },
    });

    const register = () => {
        if (!window.Alpine) return false;
        window.Alpine.data('layout_manager', layoutManager);
        window.Alpine.data('layout_manager_block', layoutManagerBlock);
        return true;
    };

    if (!register()) {
        document.addEventListener('alpine:init', register, { once: true });
    }
})();
