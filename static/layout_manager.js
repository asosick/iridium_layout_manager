// Iridium Layout Manager — client glue.
//
// Iridium's Alpine build is CSP-mode (no inline expressions), so the only way
// to wire reactivity is via a named Alpine.data component registered before
// Alpine walks the DOM. The plugin renders this script in the body BEFORE the
// x-data element AND without `defer` so it runs synchronously during HTML
// parse. That gets us in early enough to attach an `alpine:init` listener —
// Alpine fires that event right before it scans the DOM, so registering the
// data inside the listener guarantees it exists by the time x-data is
// evaluated.
//
// The fallback path covers the corner case where this script loads AFTER
// Alpine has already started (e.g. injected via htmx swap into an
// already-bootstrapped page): we register the data immediately and re-init
// the affected subtrees so they pick up the fresh registration.

(function () {
    const COMPONENT_NAME = 'layout_manager';

    const componentFactory = () => ({
        editMode: false,
        // Mirrors the first <option> by default; the form posts the bound
        // value, so this just keeps the visible select state in sync.
        selectedType: '',
        // Which page (layout) is currently shown. Drives the selector-button
        // highlight and is posted (via the hidden "layout" input) on Add.
        currentLayout: 0,
        // Total customizable pages — used to bound the cmd/ctrl+N hotkeys.
        layoutCount: 1,
        // Endpoint the grid is swapped from when switching pages.
        selectUrl: '',
        lockAfterDone: true,
        // Prevent Save from racing the session cookie written by reorder.
        reorderPending: false,
        // Bound document keydown handler, kept so we can detach on destroy.
        _hotkeyHandler: null,
        _sortHandler: null,
        _pointerDownHandler: null,
        _afterRequestHandler: null,
        _resizeState: null,
        _resizeMoveHandler: null,
        _resizeEndHandler: null,

        init() {
            const select = this.$el.querySelector('[data-lm-type-select]');
            if (select && select.options.length > 0) {
                this.selectedType = select.options[0].value;
            }

            // Read multi-page config off the root element's data-* attributes.
            const root = this.$el;
            this.selectUrl = root.dataset.lmSelectUrl || '';
            this.lockAfterDone = root.dataset.lmLockAfterDone !== 'false';
            this.editMode = root.dataset.lmEditDefault === 'true';
            this.layoutCount = parseInt(root.dataset.lmLayoutCount || '1', 10) || 1;
            this.currentLayout = parseInt(root.dataset.lmCurrentLayout || '0', 10) || 0;

            // cmd/ctrl + 1..9 switches pages (matches the Filament original).
            this._hotkeyHandler = (e) => this.onHotkey(e);
            document.addEventListener('keydown', this._hotkeyHandler);

            // Listen to SortableJS directly. Alpine's CSP evaluator can move
            // the elements while still failing to invoke an x-sort expression.
            this._sortHandler = (e) => {
                const grid = this.$el.querySelector('[data-lm-grid]');
                if (grid && e.target === grid) this.handleReorder();
            };
            this.$el.addEventListener('sort', this._sortHandler);

            this._pointerDownHandler = (e) => {
                const handle = e.target.closest('[data-lm-resize-handle]');
                if (handle && this.$el.contains(handle)) this.startResize(e, handle);
            };
            this.$el.addEventListener('pointerdown', this._pointerDownHandler);

            this._afterRequestHandler = (e) => {
                const source = e.detail && e.detail.elt;
                if (!source || !source.matches('[data-lm-done]')) return;
                if (e.detail.successful !== false && this.lockAfterDone) {
                    this.editMode = false;
                }
            };
            this.$el.addEventListener('htmx:afterRequest', this._afterRequestHandler);
        },

        destroy() {
            if (this._hotkeyHandler) {
                document.removeEventListener('keydown', this._hotkeyHandler);
                this._hotkeyHandler = null;
            }
            if (this._sortHandler) {
                this.$el.removeEventListener('sort', this._sortHandler);
                this._sortHandler = null;
            }
            if (this._pointerDownHandler) {
                this.$el.removeEventListener('pointerdown', this._pointerDownHandler);
                this._pointerDownHandler = null;
            }
            if (this._afterRequestHandler) {
                this.$el.removeEventListener('htmx:afterRequest', this._afterRequestHandler);
                this._afterRequestHandler = null;
            }
            this.cleanupResize();
        },

        // onHotkey switches pages on (cmd|ctrl)+N. Ignored when N is out of
        // range or when there's only one page. We preventDefault so the
        // page-switch wins even though some browsers map cmd+N to tab switching.
        onHotkey(e) {
            if (!(e.metaKey || e.ctrlKey) || e.altKey || e.shiftKey) return;
            if (this.layoutCount <= 1) return;
            const n = parseInt(e.key, 10);
            if (isNaN(n) || n < 1 || n > this.layoutCount) return;
            e.preventDefault();
            this.selectLayout(n - 1);
        },

        // selectLayout swaps the grid to the requested page and updates the
        // active-button highlight. The server re-renders the grid (and OOB-
        // refreshes the selector strip) for that layout, keeping its per-block
        // mutation URLs correct.
        selectLayout(i) {
            this.currentLayout = i;
            if (!this.selectUrl || !window.htmx) return;
            const grid = this.$root.querySelector('[data-lm-grid]');
            if (!grid) return;
            try {
                window.htmx.ajax('GET', this.selectUrl + '?layout=' + i, {
                    target: grid,
                    swap: 'outerHTML',
                });
            } catch (err) {
                console.error('[layoutmgr] selectLayout failed', err);
            }
        },

        toggleEdit() {
            this.editMode = !this.editMode;
        },

        startEditing() {
            this.editMode = true;
        },

        startResize(e, handle) {
            if (!this.editMode || (e.button !== undefined && e.button !== 0)) return;

            const block = handle.closest('[data-lm-block]');
            const grid = handle.closest('[data-lm-grid]');
            if (!block || !grid) return;

            e.preventDefault();
            e.stopPropagation();

            const styles = window.getComputedStyle(grid);
            const columns = Math.max(1, styles.gridTemplateColumns.split(' ').length);
            const gap = parseFloat(styles.columnGap) || 0;
            const columnWidth = (grid.clientWidth - gap * (columns - 1)) / columns;
            const unit = columnWidth + gap;
            const gridRect = grid.getBoundingClientRect();
            const blockRect = block.getBoundingClientRect();
            const startColumn = Math.max(0, Math.round((blockRect.left - gridRect.left) / unit));
            const startCols = parseInt(block.dataset.cols || '1', 10) || 1;

            this._resizeState = {
                block,
                grid,
                handle,
                startX: e.clientX,
                startCols,
                cols: startCols,
                columns,
                startColumn,
                unit,
                url: handle.dataset.resizeUrl || '',
            };
            grid.classList.add('lm-resizing');
            block.classList.add('lm-block-resizing');
            this.updateResizeGuide();

            this._resizeMoveHandler = (event) => this.previewResize(event);
            this._resizeEndHandler = (event) => this.finishResize(event);
            document.addEventListener('pointermove', this._resizeMoveHandler);
            document.addEventListener('pointerup', this._resizeEndHandler, { once: true });
            document.addEventListener('pointercancel', this._resizeEndHandler, { once: true });
        },

        previewResize(e) {
            const state = this._resizeState;
            if (!state) return;
            e.preventDefault();

            const delta = Math.round((e.clientX - state.startX) / state.unit);
            const maxCols = Math.max(1, state.columns - state.startColumn);
            state.cols = Math.max(1, Math.min(state.startCols + delta, maxCols));
            state.block.dataset.cols = String(state.cols);
            this.updateResizeGuide();
        },

        updateResizeGuide() {
            const state = this._resizeState;
            if (!state) return;
            const end = state.startColumn + state.cols;
            state.grid.querySelectorAll('.lm-grid-guide-cell').forEach((cell, index) => {
                cell.classList.toggle('is-active', index >= state.startColumn && index < end);
            });
        },

        finishResize(e) {
            const state = this._resizeState;
            if (!state) return;
            const cancelled = e.type === 'pointercancel';
            const changed = !cancelled && state.cols !== state.startCols && state.url;

            if (!changed) state.block.dataset.cols = String(state.startCols);
            this.cleanupResize();
            if (!changed || !window.htmx) return;

            window.htmx.ajax('POST', state.url + '&cols=' + state.cols, {
                source: state.handle,
                target: state.grid,
                swap: 'outerHTML',
            }).catch(err => {
                state.block.dataset.cols = String(state.startCols);
                console.error('[layoutmgr] resize failed', err);
            });
        },

        cleanupResize() {
            if (this._resizeMoveHandler) {
                document.removeEventListener('pointermove', this._resizeMoveHandler);
                this._resizeMoveHandler = null;
            }
            if (this._resizeEndHandler) {
                document.removeEventListener('pointerup', this._resizeEndHandler);
                document.removeEventListener('pointercancel', this._resizeEndHandler);
                this._resizeEndHandler = null;
            }
            if (this._resizeState) {
                this._resizeState.grid.classList.remove('lm-resizing');
                this._resizeState.block.classList.remove('lm-block-resizing');
                this._resizeState.grid.querySelectorAll('.lm-grid-guide-cell').forEach(cell => {
                    cell.classList.remove('is-active');
                });
            }
            this._resizeState = null;
        },

        // Wait until Sortable finishes its current event before reading the
        // final DOM order.
        handleReorder() {
            if (this.reorderPending) return;

            this.reorderPending = true;
            window.requestAnimationFrame(() => this.persistReorder());
        },

        persistReorder() {
            const grid = this.$el.querySelector('[data-lm-grid]');
            if (!grid) {
                this.reorderPending = false;
                return;
            }
            const order = Array.from(grid.querySelectorAll('[data-lm-block]'))
                .map(el => el.dataset.id)
                .filter(Boolean);
            const url = grid.dataset.reorderUrl;
            if (!url || order.length === 0) {
                this.reorderPending = false;
                return;
            }

            // Iridium enforces CSRF on every mutating request: a POST without a
            // valid X-CSRF-Token is rejected with 403. htmx-driven mutations
            // (add/remove/resize) get the token injected automatically via the
            // body's :hx-headers binding, but this is a RAW fetch — htmx isn't
            // involved — so we must read the current token off the Alpine
            // 'security' store and send it ourselves. Without this the reorder
            // POST is silently rejected: Alpine's sort has already moved the DOM
            // visually, so the new order LOOKS applied, but nothing persists and
            // revisiting the page shows the old order. That was the "layout not
            // placed back in the same spots" bug.
            const security = (window.Alpine && typeof window.Alpine.store === 'function')
                ? window.Alpine.store('security')
                : null;
            const headers = {
                'Content-Type': 'application/json',
                'HX-Request': 'true',
            };
            if (security && security.csrf) {
                headers['X-CSRF-Token'] = security.csrf;
            }

            fetch(url, {
                method: 'POST',
                headers,
                credentials: 'same-origin',
                body: JSON.stringify({ order }),
            })
                .then(r => {
                    // Iridium ROTATES the CSRF token on every successful mutating
                    // request and returns the fresh one in the response header.
                    // htmx normally feeds that back into the security store (via
                    // its htmx:afterRequest listener), but a raw fetch bypasses
                    // that — so we update the store ourselves. Skipping this would
                    // leave the store holding a now-stale token and 403 the NEXT
                    // htmx POST (add/remove/resize) on the page.
                    const t = r.headers.get('X-CSRF-Token');
                    if (t && security) security.csrf = t;
                    if (!r.ok) {
                        throw new Error('[layoutmgr] reorder rejected: ' + r.status);
                    }
                    return r.text();
                })
                .then(html => {
                    // Replace the grid with the server's freshly-rendered
                    // copy so order, cols, and any newly-resolved widgets
                    // come from the source of truth.
                    grid.outerHTML = html;

                    // CRITICAL: after a raw outerHTML swap, htmx and Alpine
                    // don't automatically know about the new DOM. Without
                    // re-processing, the freshly-rendered ×/+/− buttons
                    // don't get bound to htmx — so the FIRST click hits the
                    // stale (pre-reorder) button still wired up, which is
                    // why resizing widget A after a swap appeared to affect
                    // widget B for one click. We rescan both subsystems
                    // against the new grid so all the hx-post URLs the
                    // server just rendered fire correctly.
                    const newGrid = this.$el.querySelector('[data-lm-grid]');
                    if (!newGrid) return;
                    if (window.htmx && typeof window.htmx.process === 'function') {
                        try { window.htmx.process(newGrid); } catch (e) {
                            console.error('[layoutmgr] htmx.process failed', e);
                        }
                    }
                    if (window.Alpine && typeof window.Alpine.initTree === 'function') {
                        try { window.Alpine.initTree(newGrid); } catch (e) {
                            // Re-initing a tree already initialised by Alpine
                            // when x-data was processed via the parent is
                            // usually harmless; log and continue.
                            console.warn('[layoutmgr] Alpine.initTree warn', e);
                        }
                    }
                })
                .catch(err => console.error('[layoutmgr] reorder failed', err))
                .finally(() => {
                    this.reorderPending = false;
                });
        },
    });


    const register = () => {
        if (!window.Alpine) return false;
        window.Alpine.data(COMPONENT_NAME, componentFactory);
        return true;
    };

    const hasComponentState = (el) => Array.isArray(el._x_dataStack)
        && el._x_dataStack.some(state => typeof state.toggleEdit === 'function');

    const repairComponentTrees = () => {
        if (!window.Alpine || typeof window.Alpine.initTree !== 'function') return;

        document.querySelectorAll('[x-data="' + COMPONENT_NAME + '"]').forEach(el => {
            if (hasComponentState(el)) return;

            try {
                if (el._x_dataStack && typeof window.Alpine.destroyTree === 'function') {
                    window.Alpine.destroyTree(el);
                }
                window.Alpine.initTree(el);
            } catch (e) {
                console.error('[layoutmgr] component recovery failed', e);
            }
        });
    };

    // Preferred path: Alpine hasn't started yet. The alpine:init event fires
    // immediately before Alpine walks the DOM, after this script has hooked
    // it. Registering inside the listener guarantees the data component is
    // available when x-data is first evaluated.
    document.addEventListener('alpine:init', register, { once: true });

    // If Alpine already started, register immediately and repair any tree that
    // was marked initialised before the component became available. Running a
    // second check on the next frame also covers an in-progress Alpine scan.
    if (register()) {
        queueMicrotask(repairComponentTrees);
        window.requestAnimationFrame(repairComponentTrees);
    }
})();
